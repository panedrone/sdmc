package cg

import (
	"fmt"
	"strings"

	"github.com/panedrone/sdmc/internal/meta"
)

// Fields from metadata: ColumnFields.java, TableFields.java, ParamFields.java.

func pkAliases(explicitPK string) (map[string]bool, error) {
	keys, err := ListedItems(explicitPK, false)
	if err != nil {
		return nil, err
	}
	if err := checkDuplicates(keys); err != nil {
		return nil, err
	}
	res := map[string]bool{}
	for _, k := range keys {
		a, err := pkColNameAlias(strings.ToLower(k))
		if err != nil {
			return nil, err
		}
		res[a] = true
	}
	return res, nil
}

func columnFields(cols []meta.Column, model string, mode FieldNamesMode, explicitPK string) ([]FieldInfo, error) {
	var pk map[string]bool
	if strings.TrimSpace(explicitPK) != "" && explicitPK != "*" {
		var err error
		if pk, err = pkAliases(explicitPK); err != nil {
			return nil, err
		}
	}
	res := make([]FieldInfo, 0, len(cols))
	for _, c := range cols {
		fi, err := NewFieldInfo(mode, model+c.JavaType, c.Name, "q")
		if err != nil {
			return nil, err
		}
		fi = fi.WithRaw(c.RawType).WithAI(c.IsAI).WithDecimalDigits(c.Scale).WithRsMd(c.Precision, c.Scale, c.DisplaySize)
		if pk != nil {
			a, err := pkColNameAlias(c.Name)
			if err != nil {
				return nil, err
			}
			if pk[a] {
				fi = fi.WithPK(true)
			}
		}
		res = append(res, fi)
	}
	return res, nil
}

type tableFields struct {
	all, pk, notPK []FieldInfo
}

// readTableFields: explicitPK == "*" means "the PK of the table".
func readTableFields(md meta.Metadata, tm *TypeMap, mode FieldNamesMode, model, tableRef, explicitPK, auto string) (*tableFields, error) {
	t, err := md.Table(tableRef)
	if err != nil {
		return nil, err
	}
	pk := map[string]bool{}
	if explicitPK == "*" {
		names, err := md.PKColNames(tableRef)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			a, err := pkColNameAlias(n)
			if err != nil {
				return nil, err
			}
			pk[a] = true
		}
	} else if pk, err = pkAliases(explicitPK); err != nil {
		return nil, err
	}
	return createTableFields(t, pk, tm, mode, model, auto)
}

func createTableFields(t *meta.Table, pk map[string]bool, tm *TypeMap, mode FieldNamesMode, model, auto string) (*tableFields, error) {
	autoCol, autoGen := parseAutoColumn(auto)
	cols, err := columnFields(t.Columns, model, mode, "")
	if err != nil {
		return nil, err
	}
	byCol := map[string]FieldInfo{}
	for _, fi := range cols {
		byCol[fi.columnName] = fi
	}
	for _, tc := range t.TableColumns {
		if fi, ok := byCol[tc.Name]; ok {
			byCol[tc.Name] = refineByTableColumn(fi, tc, model)
		}
	}
	res := &tableFields{}
	for _, c := range t.Columns {
		fi := byCol[c.Name]
		detected, suffix := fi.renderedType, ""
		if strings.EqualFold(c.Name, autoCol) && model != "" {
			suffix = "+" + autoGen
		}
		fi = fi.WithRenderedType(tm.TargetRaw(detected+suffix, fi.rawKey(suffix)))
		a, err := pkColNameAlias(c.Name)
		if err != nil {
			return nil, err
		}
		fi = fi.WithPK(pk[a])
		res.all = append(res.all, fi)
		if pk[a] {
			res.pk = append(res.pk, fi)
		} else {
			res.notPK = append(res.notPK, fi)
		}
	}
	return res, nil
}

// parseAutoColumn — "o_id:identity" -> "o_id", "identity".
func parseAutoColumn(auto string) (string, string) {
	auto = strings.TrimSpace(auto)
	if auto == "" {
		return "", ""
	}
	parts := jsplitLit(auto, ":")
	if len(parts) < 2 {
		return auto, "auto" // the default "*" affects nothing
	}
	return parts[0], parts[1]
}

func refineByTableColumn(fi FieldInfo, tc meta.TableColumn, model string) FieldInfo {
	fi = fi.WithComment("t")
	if fi.raw == "" {
		fi = fi.WithRaw(tc.RawType) // SQLite: expressions have no type, a table column has it declared
	}
	fi = refineJdbcType(fi, tc, model)
	fi = refineColSize(fi, tc)
	if tc.DecimalDigits != nil {
		// Oracle NUMERIC is always -127, SQLite always 10
		fi = fi.WithDecimalDigits(*tc.DecimalDigits)
	}
	// IS_AUTOINCREMENT may be wrong (sqlite-jdbc-3.19.3): only if AI did not come from the columns
	if !fi.ai && tc.IsAI != nil && *tc.IsAI {
		fi = fi.WithAI(true)
	}
	if tc.IsNullable != nil {
		fi = fi.WithNullable(*tc.IsNullable)
	}
	if tc.FK != "" {
		fi = fi.WithFK(tc.FK)
	}
	if tc.IsIndexed {
		fi = fi.WithIndexed(true)
	}
	if tc.IsUnique {
		fi = fi.WithUnique(true)
	}
	return fi
}

// java.sql.Types -> Java type (org.apache.cayenne.dba.TypesMapping.SQL_ENUM_JAVA).
var sqlEnumJava = map[int]string{
	-5:   "java.lang.Long", // BIGINT
	-2:   "byte[]",         // BINARY
	-7:   "java.lang.Boolean",
	16:   "java.lang.Boolean", // BOOLEAN
	2004: "byte[]",            // BLOB
	2005: "java.lang.String",  // CLOB
	2011: "java.lang.String",  // NCLOB
	1:    "java.lang.String",  // CHAR
	-15:  "java.lang.String",  // NCHAR
	91:   "java.util.Date",    // DATE
	3:    "java.math.BigDecimal",
	8:    "java.lang.Double",
	6:    "java.lang.Float",
	4:    "java.lang.Integer",
	-1:   "java.lang.String", // LONGVARCHAR
	-16:  "java.lang.String", // LONGNVARCHAR
	-4:   "byte[]",           // LONGVARBINARY
	2:    "java.math.BigDecimal",
	7:    "java.lang.Float", // REAL
	5:    "java.lang.Short", // SMALLINT
	-6:   "java.lang.Short", // TINYINT
	92:   "java.util.Date",  // TIME
	93:   "java.util.Date",  // TIMESTAMP
	-3:   "byte[]",          // VARBINARY
	12:   "java.lang.String",
	-9:   "java.lang.String", // NVARCHAR
	2009: "java.lang.String", // SQLXML
}

func javaBySQLType(t int) string {
	if j, ok := sqlEnumJava[t]; ok {
		return j
	}
	switch t { // Oracle
	case -100: // TIMESTAMPNS
		return "java.util.Date"
	case -101: // TIMESTAMPTZ
		return "java.time.ZonedDateTime"
	case -102: // TIMESTAMPLTZ
		return "java.time.LocalDateTime"
	}
	return meta.JavaObject
}

func refineJdbcType(fi FieldInfo, tc meta.TableColumn, model string) FieldInfo {
	// the type is overridden only if Object came from the column: no conversions at runtime
	if fi.originalType != model+meta.JavaObject || tc.DataType == nil {
		return fi
	}
	j := javaBySQLType(*tc.DataType)
	return fi.WithScalarType(j).WithRenderedType(model + j)
}

func refineColSize(fi FieldInfo, tc meta.TableColumn) FieldInfo {
	if fi.scalarType != "java.lang.String" || tc.ColumnSize == nil {
		return fi
	}
	const maxSize = 0x3FFF // 16K
	size := *tc.ColumnSize
	if size > maxSize { // sqlite3: 2000000000
		size = 0
	}
	if p := fi.precision; p > 0 && p < maxSize && (size == 0 || p < size) {
		size = p
	}
	return fi.WithColumnSize(size)
}

// Parameters -------------------------------------------------------

// readParams is not for CRUD.
func readParams(md meta.Metadata, tm *TypeMap, mc *Macros, jdbcSQL string, mode FieldNamesMode, descriptors []string) ([]ParamInfo, error) {
	if err := checkDuplicates(descriptors); err != nil {
		return nil, err
	}
	ps, err := md.SQLParams(jdbcSQL, len(descriptors))
	if err != nil {
		return nil, err
	}
	var res []ParamInfo
	if ps.CountUnknown {
		for _, d := range descriptors {
			p, err := createParamInfo(tm, mc, mode, d, nil)
			if err != nil {
				return nil, err
			}
			res = append(res, p)
		}
		return res, nil
	}
	plain := 0
	for i, d := range descriptors {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, "[") {
			// an array of implicit cursor handlers is declared, but it does not count as a JDBC parameter
			if !strings.HasSuffix(d, "]") {
				return nil, fmt.Errorf("Ending ']' expected")
			}
			continue
		}
		plain++
		// a parameter that looks like a column: type-map macros expect a column
		base, err := NewFieldInfo(mode, ps.JavaTypes[i], "base_fi", "base_fi")
		if err != nil {
			return nil, err
		}
		if i < len(ps.RawTypes) {
			base = base.WithRaw(ps.RawTypes[i])
		}
		p, err := createParamInfo(tm, mc, mode, d, &base)
		if err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	if ps.Count != plain {
		return nil, fmt.Errorf("Parameters declared in method: %d, detected by MetaData: %d", len(descriptors), ps.Count)
	}
	return res, nil
}

// createParamInfo: base (may be nil) is the column the parameter is compared with; its type is the default.
func createParamInfo(tm *TypeMap, mc *Macros, mode FieldNamesMode, descriptor string, base *FieldInfo) (ParamInfo, error) {
	typ, name, _ := splitParamTypeAndName(descriptor)
	var target string
	if strings.TrimSpace(typ) == "" {
		if base == nil {
			target = tm.TargetRaw(meta.JavaObject, meta.RawUnknown)
		} else {
			var err error
			target, err = mc.ParseTargetType(tm.TargetOf(*base), *base)
			if err != nil {
				return ParamInfo{}, err
			}
		}
	} else if tm.Unmapped(typ) { // a Java name without a key: in the driver's words
		target = tm.TargetRaw(typ, tm.RawOfJava(typ))
	} else {
		target = tm.Target(typ)
	}
	return NewParamInfo(mode, meta.JavaObject, name).WithType(target), nil
}
