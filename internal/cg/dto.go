package cg

import (
	"fmt"
	"strings"

	"github.com/panedrone/go-sdm/internal/meta"
	"github.com/panedrone/go-sdm/internal/sdmxml"
)

// Core is what the Java plug-in keeps in JdbcUtils for one generation run:
// metadata, type-map, macros and the naming mode of DTO fields.
type Core struct {
	md        meta.Metadata
	tm        *TypeMap
	mc        *Macros
	dtoMode   FieldNamesMode // names of DTO fields
	paramMode FieldNamesMode // names of exec-dml method parameters
	sqlRoot   string
	crud      map[string]*tableFields // read/update/delete of one <crud> ask about one table
}

func NewCore(md meta.Metadata, s *sdmxml.Settings, dtoMode, paramMode FieldNamesMode, sqlRoot string) *Core {
	return &Core{
		md:        md,
		tm:        newTypeMapFor(md, s.TypeMap),
		mc:        NewMacros(s.Macros),
		dtoMode:   dtoMode,
		paramMode: paramMode,
		sqlRoot:   sqlRoot,
		crud:      map[string]*tableFields{},
	}
}

func (c *Core) DtoFieldNamesMode() FieldNamesMode { return c.dtoMode }
func (c *Core) SQLRoot() string                   { return c.sqlRoot }

// TargetTypeByTypeMap returns the type without the assign function.
func (c *Core) TargetTypeByTypeMap(detected string) string {
	t, _, _ := SplitTargetType(c.tm.Target(detected))
	return t
}

// DtoFields returns the fields of a DTO class.
func (c *Core) DtoFields(dto *sdmxml.DtoClass) ([]FieldInfo, error) {
	return c.dtoFields(false, dto)
}

func (c *Core) dtoFields(ignoreModel bool, dto *sdmxml.DtoClass) ([]FieldInfo, error) {
	fields, err := c.fieldsByRef(ignoreModel, dto)
	if err == nil {
		fields, err = c.refineByFieldXML(ignoreModel, dto, fields)
	}
	if err != nil {
		return nil, fmt.Errorf("<dto-class name=\"%s\">: %w", dto.Name, err)
	}
	res := make([]FieldInfo, len(fields))
	for i, fi := range fields {
		res[i] = fi.WithScalarType(c.tm.Target(fi.scalarType))
	}
	return res, nil
}

func (c *Core) fieldsByRef(ignoreModel bool, dto *sdmxml.DtoClass) ([]FieldInfo, error) {
	model := ""
	if !ignoreModel {
		model = Model(dto.Name)
	}
	if model != "" {
		model += "-" // prefix of type names: "sa-int"
	}
	ref := dto.Ref
	if IsEmptyRef(ref) {
		return nil, nil
	}
	jdbcSQL, err := JdbcSQLByDtoClassRef(ref, c.sqlRoot)
	if err != nil {
		return nil, err
	}
	// fields from SQL first: invisible and database-specific columns will not get in
	cols, err := c.md.SQLColumns(jdbcSQL)
	if err != nil {
		return nil, err
	}
	sqlFields, err := columnFields(cols, model, c.dtoMode, dto.PK)
	if err != nil {
		return nil, err
	}
	var table string
	switch {
	case IsTableRef(ref):
		table = ref
	case IsSQLShortcutRef(ref):
		shc, err := parseSQLShortcutRef(ref)
		if err != nil {
			return nil, err
		}
		table = shc.tableName
	default:
		return sqlFields, nil
	}
	// table columns returned by the SQL, but with the table metadata
	sqlCols := map[string]bool{}
	for _, n := range columnNames(sqlFields) {
		sqlCols[n] = true
	}
	t, err := readTableFields(c.md, c.tm, c.dtoMode, model, table, "*", dto.Auto)
	if err != nil {
		return nil, err
	}
	var res []FieldInfo
	for _, fi := range t.all {
		if sqlCols[fi.columnName] {
			res = append(res, fi)
		}
	}
	return res, nil
}

func (c *Core) refineByFieldXML(ignoreModel bool, dto *sdmxml.DtoClass, fields []FieldInfo) ([]FieldInfo, error) {
	res := append([]FieldInfo(nil), fields...)
	// a column is refined where it was met last (if the SQL returned it twice)
	index := map[string]int{}
	for i, fi := range res {
		index[fi.columnName] = i
	}
	refined := map[string]bool{}
	excluded := map[int]bool{}
	for _, xf := range dto.Fields {
		xt := xf.Type
		i, ok := index[xf.Column]
		if !ok { // declared in XML, but the SQL did not return it
			fi, err := NewFieldInfo(c.dtoMode, xt, xf.Column, "xml("+xf.Column+")")
			if err != nil {
				return nil, err
			}
			if c.tm.Unmapped(xt) { // a Java name without a key and without metadata: in the driver's words
				fi = fi.WithRaw(c.tm.RawOfJava(xt))
				if !ignoreModel {
					fi = fi.withModel(Model(dto.Name))
				}
			}
			res = append(res, fi)
			i = len(res) - 1
			index[xf.Column] = i
		}
		if strings.TrimSpace(xf.Type) == "-" {
			excluded[i] = true
		}
		if ok && c.tm.Unmapped(xt) {
			xt = res[i].originalType // a Java name without a key: the column type from metadata
		}
		fi, err := c.refineByTypeMapAndMacros(res[i], xt, false)
		if err != nil {
			return nil, err
		}
		res[i] = fi.WithScalarType(xt)
		refined[xf.Column] = true
	}
	var out []FieldInfo
	for i, fi := range res {
		if excluded[i] {
			continue
		}
		if !refined[fi.columnName] {
			var err error
			// Type() exactly, not OriginalType(); a type mapped already is not mapped to "default" once more
			typ := strings.TrimSpace(fi.renderedType)
			if fi, err = c.refineByTypeMapAndMacros(fi, typ, typ != strings.TrimSpace(fi.originalType)); err != nil {
				return nil, err
			}
		}
		out = append(out, fi)
	}
	return out, nil
}

// refineByTypeMapAndMacros: mapped is a type that is a target already (TypeMap.TargetMapped).
func (c *Core) refineByTypeMapAndMacros(fi FieldInfo, typeName string, mapped bool) (FieldInfo, error) {
	typeName = strings.TrimSpace(typeName)
	var target string
	var err error
	switch start := strings.IndexByte(typeName, '|'); {
	case start == -1: // type only: "int64"
		t := c.tm.Target(typeName)
		switch {
		case mapped:
			t = c.tm.TargetMapped(typeName)
		case typeName == strings.TrimSpace(fi.originalType): // first pass: the type from metadata
			t = c.tm.TargetRaw(typeName, fi.rawKey(""))
		}
		target, err = c.mc.ParseTargetType(t, fi)
	case start == 0: // "|0: hello0|1:hello1": to the type from metadata
		target, err = c.mc.ParseTargetType(c.tm.TargetRaw(fi.originalType, fi.rawKey(""))+typeName, fi)
	default: // "{type}|0:Integer|1:hello1"
		target, err = c.mc.ParseTargetType(typeName, fi)
	}
	if err != nil {
		return fi, err
	}
	return fi.WithRenderedType(target), nil
}
