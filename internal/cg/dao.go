package cg

import (
	"errors"
	"fmt"
	"strings"

	"github.com/panedrone/sdmc/internal/meta"
	"github.com/panedrone/sdmc/internal/sdmxml"
)

// DaoSqlInfo is the SQL of a method, the returned fields and the parameters (JdbcUtils.DaoSqlInfo).
// HasSQL == false: the method is not rendered, a warning comment is rendered instead.
type DaoSqlInfo struct {
	SQL       string
	HasSQL    bool
	Fields    []FieldInfo // query, CRUD read: result columns; CRUD create/update: assigned ones
	Params    []ParamInfo // query: parameters; create: values; read/update/delete: PK
	Generated []FieldInfo // CRUD create: generated columns
}

func findDto(name string, dtos []sdmxml.DtoClass) (*sdmxml.DtoClass, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("Invalid DTO class name: " + name)
	}
	for i := range dtos {
		if dtos[i].Name == name {
			return &dtos[i], nil
		}
	}
	return nil, errors.New("XML element not found <dto-class name=\"" + name + "\"")
}

// FindDto is for the generators.
func FindDto(name string, dtos []sdmxml.DtoClass) (*sdmxml.DtoClass, error) {
	return findDto(name, dtos)
}

// Raw-SQL ---------------------------------------------------------

// QueryInfo — <query...>, <query-list...>, <query-dto...>, <query-dto-list...>.
func (c *Core) QueryInfo(ref string, descriptors []string, dtoOrReturnType string, returnTypeIsDto bool, dtos []sdmxml.DtoClass) (*DaoSqlInfo, error) {
	if err := checkDuplicates(descriptors); err != nil {
		return nil, err
	}
	ref, err := refineDaoRef(dtoOrReturnType, returnTypeIsDto, dtos, ref)
	if err != nil {
		return nil, err
	}
	jdbcSQL, err := JdbcSQLByRef(ref, c.sqlRoot)
	if err != nil {
		return nil, err
	}
	paramMode := AsIs // dto_param_type is always empty in Go
	var fields []FieldInfo
	var params []ParamInfo
	if IsSQLShortcutRef(ref) {
		shc, err := parseSQLShortcutRef(ref)
		if err != nil {
			return nil, err
		}
		var dto *sdmxml.DtoClass
		if returnTypeIsDto {
			if dto, err = findDto(dtoOrReturnType, dtos); err != nil {
				return nil, err
			}
		}
		t, err := readTableFields(c.md, c.tm, c.dtoMode, "", shc.tableName, "*", "")
		if err != nil {
			return nil, err
		}
		if params, err = c.shortcutParams(shc, paramMode, descriptors, t.all); err != nil {
			return nil, err
		}
		if fields, err = c.shortcutFields(shc, dto, dtoOrReturnType, t.all); err != nil {
			return nil, err
		}
	} else {
		if fields, err = c.customSQLFields(jdbcSQL, dtoOrReturnType, returnTypeIsDto, dtos); err != nil {
			return nil, err
		}
		if params, err = readParams(c.md, c.tm, c.mc, jdbcSQL, paramMode, descriptors); err != nil {
			return nil, err
		}
	}
	res := make([]FieldInfo, len(fields))
	for i, fi := range fields {
		res[i] = fi.WithRenderedType(c.tm.TargetOf(fi))
	}
	return &DaoSqlInfo{SQL: jdbcSQL, HasSQL: true, Fields: res, Params: params}, nil
}

func (c *Core) ExecDmlParams(jdbcSQL string, descriptors []string) ([]ParamInfo, error) {
	return readParams(c.md, c.tm, c.mc, jdbcSQL, c.paramMode, descriptors)
}

func refineDaoRef(dtoOrReturnType string, returnTypeIsDto bool, dtos []sdmxml.DtoClass, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		if !returnTypeIsDto {
			return "", errors.New("Empty 'ref' is not allowed here")
		}
		dto, err := findDto(dtoOrReturnType, dtos)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(dto.Ref) == "" {
			return "", errors.New("Both DAO 'ref' and DTO 'ref' are empty")
		}
		ref = dto.Ref
	}
	return strings.TrimSpace(ref), nil
}

func (c *Core) returnedFieldInfo(explicitRetType string, daoFields []FieldInfo) (FieldInfo, error) {
	col, typ := "ret_value", meta.JavaObject
	// a Java name of the plug-in without a key in type-map gives way to the type of the column
	replaced := c.tm.Unmapped(explicitRetType) && len(daoFields) > 0
	switch {
	case strings.TrimSpace(explicitRetType) != "" && !replaced:
		typ = explicitRetType
		// the type is the XML's, the column is the query's: the plug-in names it "ret_value" here, and
		// a data store that reads columns by name (C++) finds nothing, the value is always NULL
		if len(daoFields) > 0 {
			col = daoFields[0].columnName
		}
	case len(daoFields) > 0: // no return-type, or a replaced one
		col, typ = daoFields[0].columnName, daoFields[0].renderedType
	}
	fi, err := NewFieldInfo(c.dtoMode, typ, col, "returned-value")
	if err == nil && c.tm.Unmapped(typ) { // a Java name without a key and without columns: in the driver's words
		fi = fi.WithRaw(c.tm.RawOfJava(typ))
	}
	return fi, err
}

// refineDaoFieldsByDtoFields keeps the DAO fields that the DTO has, with its types; the rest are excluded.
func (c *Core) refineDaoFieldsByDtoFields(dto *sdmxml.DtoClass, daoFields []FieldInfo) ([]FieldInfo, error) {
	list, err := c.dtoFields(true, dto)
	if err != nil {
		return nil, err
	}
	dtoFields := byColumnName(list)
	objectTarget := c.tm.Target(meta.JavaObject)
	var res []FieldInfo
	for _, daoFi := range daoFields {
		dtoFi, ok := dtoFields[daoFi.columnName]
		if !ok {
			continue
		}
		fi := daoFi
		// the DTO field type takes precedence if it is not Object or its target type is not "object"
		if dtoFi.originalType != meta.JavaObject || (objectTarget != "" && objectTarget != dtoFi.renderedType) {
			fi = fi.WithRenderedType(dtoFi.renderedType).WithAssignFunc(dtoFi.assignFunc)
		}
		res = append(res, fi.WithComment(dtoFi.comment+" <- "+daoFi.comment))
	}
	return res, nil
}

// SQL-shortcut ----------------------------------------------------

func (c *Core) shortcutParams(shc sqlShortcut, mode FieldNamesMode, descriptors []string, tableFields []FieldInfo) ([]ParamInfo, error) {
	if !shc.hasParams {
		return nil, nil
	}
	filter, err := ListedItems(shc.params, false)
	if err != nil {
		return nil, err
	}
	if err := checkDuplicates(filter); err != nil {
		return nil, err
	}
	cols := byColumnName(tableFields)
	var byFilter []FieldInfo
	for _, f := range filter {
		fi, ok := cols[f]
		if !ok {
			return nil, errors.New("'" + f + "' not found among table columns [" +
				strings.Join(columnNames(tableFields), ", ") + "]. Ensure upper/lower case.")
		}
		byFilter = append(byFilter, fi)
	}
	// parameter types come from the table, not refined by the DTO
	if len(descriptors) != len(byFilter) {
		return nil, fmt.Errorf("Invalid SQL-shortcut: method params declared: %d, but SQL params expected: %d",
			len(descriptors), len(byFilter))
	}
	var res []ParamInfo
	for i, d := range descriptors {
		p, err := createParamInfo(c.tm, c.mc, mode, d, &byFilter[i])
		if err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	return res, nil
}

func (c *Core) shortcutFields(shc sqlShortcut, dto *sdmxml.DtoClass, returnType string, tableFields []FieldInfo) ([]FieldInfo, error) {
	var res []FieldInfo
	if !shc.hasCols {
		res = append(res, tableFields...)
	} else {
		cc, err := ListedItems(shc.colNames, false)
		if err != nil {
			return nil, err
		}
		set := map[string]bool{}
		for _, n := range cc {
			set[n] = true
		}
		for _, fi := range tableFields {
			if set[fi.columnName] {
				res = append(res, fi)
			}
		}
	}
	if dto != nil {
		var err error
		if res, err = c.refineDaoFieldsByDtoFields(dto, res); err != nil {
			return nil, err
		}
	} else { // scalar result: no DTO name
		fi, err := c.returnedFieldInfo(returnType, tableFields)
		if err != nil {
			return nil, err
		}
		res = append(res, fi)
	}
	if len(res) > 0 {
		res[0] = res[0].WithComment(res[0].comment + " [INFO] SQL-shortcut")
	}
	return res, nil
}

// Custom SQL ------------------------------------------------------

func (c *Core) customSQLFields(jdbcSQL, dtoOrReturnType string, returnTypeIsDto bool, dtos []sdmxml.DtoClass) ([]FieldInfo, error) {
	var daoFields []FieldInfo
	errText := ""
	func() {
		cols, err := c.md.SQLColumns(jdbcSQL)
		if err == nil {
			daoFields, err = columnFields(cols, "", c.dtoMode, "") // no model
		}
		for i := 0; err == nil && i < len(daoFields); i++ {
			var t string
			t, err = c.mc.ParseTargetType(c.tm.TargetRaw(daoFields[i].renderedType, daoFields[i].rawKey("")), daoFields[i])
			daoFields[i] = daoFields[i].WithRenderedType(t)
		}
		if err != nil {
			errText = err.Error()
			if errText == "" {
				errText = "null"
			}
		}
	}()
	if !returnTypeIsDto {
		fi, err := c.returnedFieldInfo(dtoOrReturnType, daoFields)
		if err != nil {
			return nil, err
		}
		return []FieldInfo{fi}, nil
	}
	// not only tables, hence the DTO fields, not the table ones
	dto, err := findDto(dtoOrReturnType, dtos)
	if err != nil {
		return nil, err
	}
	dtoFields, err := c.dtoFields(true, dto)
	if err != nil {
		return nil, err
	}
	var res []FieldInfo
	switch {
	case len(daoFields) == 0:
		// the SQL gave no fields (CALL, for example): the DTO fields are taken
		res = append(res, dtoFields...)
		if errText != "" && len(res) > 0 {
			res[0] = res[0].WithComment(res[0].comment + " [INFO] " +
				strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(errText))))
		}
	case daoFields[0].renderedType == "java.sql.ResultSet":
		// PostgreSQL: 'select * from get_tests_by_rating_rc(?)' (a UDF returning REF_CURSOR)
		res = append(res, dtoFields...)
		comment := " [INFO] Column 0 is of type ResultSet"
		if errText != "" {
			comment += ", " + errText
		}
		res[0] = res[0].WithComment(res[0].comment + comment)
	default:
		byCol := byColumnName(dtoFields)
		for _, daoFi := range daoFields {
			if dtoFi, ok := byCol[daoFi.columnName]; ok {
				res = append(res, dtoFi.WithComment(dtoFi.comment+" <- "+daoFi.comment))
			}
		}
	}
	if len(res) == 0 {
		return nil, errors.New("DAO columns [" + strings.Join(columnNames(daoFields), ", ") +
			"] not found among DTO columns [" + strings.Join(columnNames(dtoFields), ", ") + "].")
	}
	return res, nil
}

// CRUD ------------------------------------------------------------

func (c *Core) CrudCreateInfo(table string, dto *sdmxml.DtoClass, generated string) (*DaoSqlInfo, error) {
	auto := map[string]bool{}
	if generated != "*" {
		keys, err := ListedItems(generated, false)
		if err != nil {
			return nil, err
		}
		if err := checkDuplicates(keys); err != nil {
			return nil, err
		}
		for _, k := range keys {
			auto[strings.ToLower(k)] = true
		}
	}
	t, err := readTableFields(c.md, c.tm, c.dtoMode, "", table, "*", dto.Auto)
	if err != nil {
		return nil, err
	}
	var notGen, gen []FieldInfo
	for _, fi := range t.all {
		lo := strings.ToLower(fi.columnName)
		switch {
		case auto[lo]: // must be empty in the end
			delete(auto, lo)
			gen = append(gen, fi.WithAI(true))
		case fi.ai:
			gen = append(gen, fi)
		default:
			notGen = append(notGen, fi)
		}
	}
	if len(auto) > 0 {
		var rest []string
		for k := range auto {
			rest = append(rest, k)
		}
		return nil, fmt.Errorf("Unknown columns are listed as 'auto': [%s]", strings.Join(rest, ", "))
	}
	sql, err := crudCreateSQL(table, notGen)
	if err != nil {
		return nil, err
	}
	return &DaoSqlInfo{SQL: sql, HasSQL: true, Fields: notGen, Params: ParamsOf(notGen), Generated: gen}, nil
}

func (c *Core) CrudReadInfo(table string, dto *sdmxml.DtoClass, fetchList bool, explicitPK string) (*DaoSqlInfo, error) {
	t, err := c.crudTable(dto, table, explicitPK)
	if err != nil {
		return nil, err
	}
	sql, err := crudReadSQL(table, t.pk, fetchList)
	if err != nil {
		return nil, err
	}
	return &DaoSqlInfo{SQL: sql, HasSQL: true, Fields: t.all, Params: ParamsOf(t.pk)}, nil
}

func (c *Core) CrudUpdateInfo(table string, dto *sdmxml.DtoClass, explicitPK string) (*DaoSqlInfo, error) {
	t, err := c.crudTable(dto, table, explicitPK)
	if err != nil {
		return nil, err
	}
	res := &DaoSqlInfo{Fields: t.notPK, Params: ParamsOf(t.pk)}
	if len(t.notPK) > 0 && len(t.pk) > 0 {
		res.SQL, res.HasSQL = crudUpdateSQL(table, t.notPK, t.pk), true
	}
	return res, nil
}

func (c *Core) CrudDeleteInfo(table string, dto *sdmxml.DtoClass, explicitPK string) (*DaoSqlInfo, error) {
	t, err := c.crudTable(dto, table, explicitPK)
	if err != nil {
		return nil, err
	}
	res := &DaoSqlInfo{Params: ParamsOf(t.pk)}
	if len(t.pk) > 0 {
		res.SQL, res.HasSQL = crudDeleteSQL(table, t.pk), true
	}
	return res, nil
}

func (c *Core) crudTable(dto *sdmxml.DtoClass, table, explicitPK string) (*tableFields, error) {
	key := dto.Name + "|" + table + "|" + explicitPK
	if t, ok := c.crud[key]; ok {
		return t, nil
	}
	t, err := readTableFields(c.md, c.tm, c.dtoMode, "", table, explicitPK, dto.Auto)
	if err != nil {
		return nil, err
	}
	all, err := c.refineDaoFieldsByDtoFields(dto, t.all)
	if err != nil {
		return nil, err
	}
	// columns missing from the DTO are excluded from all only: pk and notPK stay as they are
	refined := byColumnName(all)
	take := func(fields []FieldInfo) []FieldInfo {
		res := make([]FieldInfo, len(fields))
		for i, fi := range fields {
			if r, ok := refined[fi.columnName]; ok {
				res[i] = r
			} else {
				res[i] = fi
			}
		}
		return res
	}
	res := &tableFields{all: all, pk: take(t.pk), notPK: take(t.notPK)}
	c.crud[key] = res
	return res, nil
}
