package cg

import (
	"errors"
	"strings"

	"github.com/panedrone/sdmc/internal/meta"
)

// FieldInfo is a table column, a result column or a DTO field (FieldInfo.java).
// It is copied by value, and each With... returns a modified copy: the holder of
// a FieldInfo can be sure nobody changes it behind its back.
type FieldInfo struct {
	name               string // rendered_name
	lowerCamel         string
	renderedType       string
	originalType       string
	raw                string // the type in the driver's words, without the model; "" means the driver did not report it
	originalScalarType string
	scalarType         string
	model              string
	columnName         string
	columnSize         int
	decimalDigits      int
	ai                 bool
	pk                 bool
	nullable           bool
	fk                 string
	indexed            bool
	unique             bool
	comment            string
	namePrefix         string
	assignFunc         string
	precision          int
	scale              int
	displaySize        int
}

var renderedNameReplacer = strings.NewReplacer(
	"-", "_", // MySQL: `api-key`
	".", "_", // [OrderDetails].OrderID
	":", "_", // CustomerID:1 -- xerial SQLite3
	" ", "_", // MySQL
	"[", "", // [OrderDetails].OrderID
	"]", "",
)

func NewFieldInfo(mode FieldNamesMode, originalType, colName, comment string) (FieldInfo, error) {
	f := FieldInfo{originalType: originalType, columnName: colName, comment: comment}
	if strings.TrimSpace(originalType) == "-" {
		f.originalScalarType = originalType // an excluded field
	} else {
		parts := jsplitLit(originalType, "-")
		switch len(parts) {
		case 2:
			f.model = parts[0]
			f.originalScalarType = parts[1]
		case 1:
			f.originalScalarType = originalType
		default:
			return f, errors.New("Invalid field type: " + originalType)
		}
	}
	f.scalarType = f.originalScalarType
	f.renderedType = originalType
	f.name = colName
	if f.name != "" {
		f.name = renderedNameReplacer.Replace(f.name)
		f.lowerCamel = LowerCamel(f.name)
		switch mode {
		case LowerCamelCase:
			f.name = LowerCamel(f.name)
		case TitleCase:
			f.name = Title(f.name)
		case SnakeCase:
			f.name = CamelCaseToLowerSnakeCase(f.name)
			f.namePrefix = "_"
		}
	}
	return f, nil
}

// WithRenderedType takes "type->assign_func" from type-map: the type and the assign function.
func (f FieldInfo) WithRenderedType(t string) FieldInfo {
	typ, assign, ok := SplitTargetType(t)
	f.renderedType = typ
	if ok {
		f.assignFunc = assign
	}
	return f
}

func (f FieldInfo) WithAssignFunc(s string) FieldInfo { f.assignFunc = s; return f }
func (f FieldInfo) WithRaw(s string) FieldInfo        { f.raw = s; return f }
func (f FieldInfo) WithName(s string) FieldInfo       { f.name = s; return f }
func (f FieldInfo) WithComment(s string) FieldInfo    { f.comment = s; return f }
func (f FieldInfo) WithScalarType(s string) FieldInfo { f.scalarType = s; return f }
func (f FieldInfo) WithPK(b bool) FieldInfo           { f.pk = b; return f }
func (f FieldInfo) WithAI(b bool) FieldInfo           { f.ai = b; return f }
func (f FieldInfo) WithFK(s string) FieldInfo         { f.fk = s; return f }
func (f FieldInfo) WithNullable(b bool) FieldInfo     { f.nullable = b; return f }
func (f FieldInfo) WithIndexed(b bool) FieldInfo      { f.indexed = b; return f }
func (f FieldInfo) WithUnique(b bool) FieldInfo       { f.unique = b; return f }
func (f FieldInfo) WithColumnSize(n int) FieldInfo    { f.columnSize = n; return f }
func (f FieldInfo) WithDecimalDigits(n int) FieldInfo { f.decimalDigits = n; return f }

// WithRsMd sets the ResultSetMetaData of the column: they always come together.
func (f FieldInfo) WithRsMd(precision, scale, displaySize int) FieldInfo {
	f.precision, f.scale, f.displaySize = precision, scale, displaySize
	return f
}

// rawKey is the raw name as a type-map key, with the class model: "gorm-int4" ("" if none).
// When the driver did not report the type (MySQL parameters, SQLite expressions), it is meta.RawUnknown.
func (f FieldInfo) rawKey(suffix string) string {
	raw := f.raw
	if raw == "" {
		if !strings.HasSuffix(f.originalType, meta.JavaObject) {
			return ""
		}
		raw = meta.RawUnknown
	}
	if f.model != "" {
		return f.model + "-" + raw + suffix
	}
	return raw + suffix
}

// For the templates.
func (f FieldInfo) Name() string           { return f.name }
func (f FieldInfo) LowerCamelName() string { return f.lowerCamel }
func (f FieldInfo) Type() string           { return f.renderedType }
func (f FieldInfo) ColumnName() string     { return f.columnName }
func (f FieldInfo) AssignFunc() string     { return f.assignFunc }

func (f FieldInfo) IsAI() bool                 { return f.ai }
func (f FieldInfo) IsPK() bool                 { return f.pk }
func (f FieldInfo) Comment() string            { return f.comment }
func (f FieldInfo) OriginalType() string       { return f.originalType }
func (f FieldInfo) ScalarType() string         { return f.scalarType }
func (f FieldInfo) OriginalScalarType() string { return f.originalScalarType }
func (f FieldInfo) Precision() int             { return f.precision }
func (f FieldInfo) Scale() int                 { return f.scale }

func (f FieldInfo) ColumnSize() int {
	if f.ai {
		return 0
	}
	return f.columnSize
}

func columnNames(fields []FieldInfo) []string {
	res := make([]string, len(fields))
	for i, f := range fields {
		res[i] = f.columnName
	}
	return res
}

// byColumnName maps "column -> field"; if the SQL returned a column twice, the last one wins.
func byColumnName(fields []FieldInfo) map[string]FieldInfo {
	res := make(map[string]FieldInfo, len(fields))
	for _, f := range fields {
		res[f.columnName] = f
	}
	return res
}

// ParamInfo is a method parameter (ParamInfo.java).
type ParamInfo struct {
	name         string
	lowerCamel   string
	renderedType string
	namePrefix   string
}

// isRenderedExpression: renderers of implicit cursors pass an expression instead of a name.
func isRenderedExpression(name string) bool {
	return strings.Contains(name, "func(") || strings.Contains(name, "new RowHandler[]")
}

var paramNameReplacer = strings.NewReplacer("-", "_", ".", "_", ":", "_")

func NewParamInfo(mode FieldNamesMode, typ, name string) ParamInfo {
	p := ParamInfo{name: name, renderedType: typ}
	if name != "" && !isRenderedExpression(name) {
		p.name = paramNameReplacer.Replace(p.name)
		p.lowerCamel = LowerCamel(p.name)
		switch mode {
		case LowerCamelCase:
			p.name = LowerCamel(p.name)
		case TitleCase:
			p.name = Title(p.name)
		case SnakeCase:
			p.name = CamelCaseToLowerSnakeCase(p.name)
			p.namePrefix = "_"
		}
	}
	return p
}

// ParamOf returns a column as a parameter: the same name and type.
func ParamOf(f FieldInfo) ParamInfo {
	return ParamInfo{name: f.name, lowerCamel: f.lowerCamel, renderedType: f.renderedType, namePrefix: f.namePrefix}
}

func ParamsOf(fields []FieldInfo) []ParamInfo {
	res := make([]ParamInfo, len(fields))
	for i, f := range fields {
		res[i] = ParamOf(f)
	}
	return res
}

// WithType: a parameter needs only the type from "type->assign_func".
func (p ParamInfo) WithType(t string) ParamInfo {
	p.renderedType, _, _ = SplitTargetType(t)
	return p
}

func (p ParamInfo) Name() string           { return p.name }
func (p ParamInfo) LowerCamelName() string { return p.lowerCamel }
func (p ParamInfo) Type() string           { return p.renderedType }

// withModel sets the class model ("gorm") if the type does not carry it: for fields declared in XML only.
func (f FieldInfo) withModel(m string) FieldInfo {
	if f.model == "" {
		f.model = m
	}
	return f
}
