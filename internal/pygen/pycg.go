// Package pygen is the Python generator: the port of PythonCG.java and python.vm for no ORM.
// The DTO and DAO of a Python project go to one package, <folders target> of settings.xml.
package pygen

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/panedrone/sdmc/internal/cg"
	"github.com/panedrone/sdmc/internal/sdmxml"
)

//go:embed python.tmpl
var pyTmpl string

var tmpl = template.Must(template.New("python").Parse(pyTmpl))

func render(name string, data any) (string, error) {
	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func checkSettings(s *sdmxml.Settings) error {
	if strings.TrimSpace(s.Dto.Macro) != "" || strings.TrimSpace(s.Dao.Macro) != "" {
		return errors.New("custom Velocity templates (<dto macro=...>, <dao macro=...>) are not supported by sdmc")
	}
	return nil
}

// checkNoModel: a model of a class (sa-Task, dj-Task, ...) is an ORM, Python has no ORM in sdmc.
func checkNoModel(className string) error {
	if m := cg.Model(className); m != "" {
		return fmt.Errorf("%s: the model %q is an ORM; sdmc generates Python without ORM, use %s",
			className, m, cg.ClassNameWithoutModel(className))
	}
	return nil
}

// Package is the Python package of <folders target>: dbal/models -> dbal.models.
func Package(s *sdmxml.Settings) string {
	return strings.NewReplacer("/", ".", `\`, ".").Replace(s.Folders.Target)
}

// DTO -------------------------------------------------------------

type DTO struct {
	core *cg.Core
	dtos []sdmxml.DtoClass
}

func NewDTO(core *cg.Core, s *sdmxml.Settings, sdm *sdmxml.Sdm) (*DTO, error) {
	if err := checkSettings(s); err != nil {
		return nil, err
	}
	return &DTO{core: core, dtos: sdm.DtoClasses}, nil
}

type dtoData struct {
	ClassName string
	HasHeader bool
	Header    string
	Fields    []cg.FieldInfo
}

func (g *DTO) Translate(className string) (string, error) {
	if err := checkNoModel(className); err != nil {
		return "", err
	}
	dto, err := cg.FindDto(className, g.dtos)
	if err != nil {
		return "", err
	}
	fields, err := g.core.DtoFields(dto)
	if err != nil {
		return "", err
	}
	d := dtoData{ClassName: className, Fields: fields}
	if dto.Header != nil {
		d.HasHeader, d.Header = true, *dto.Header
	}
	text, err := render("dto_class", d)
	return strings.ReplaceAll(text, "\r\n", "\n"), err
}

// DAO -------------------------------------------------------------

type DAO struct {
	core       *cg.Core
	dtos       []sdmxml.DtoClass
	dtoPackage string
	imports    map[string]importItem // by class name
}

type importItem struct{ File, Class string }

func NewDAO(core *cg.Core, s *sdmxml.Settings, sdm *sdmxml.Sdm) (*DAO, error) {
	if err := checkSettings(s); err != nil {
		return nil, err
	}
	return &DAO{core: core, dtos: sdm.DtoClasses, dtoPackage: Package(s)}, nil
}

type daoData struct {
	Imports   []importItem
	ClassName string
	Methods   []string
}

type mapping struct {
	MethodParamName  string
	ExecDmlParamName string
	DtoClassName     string
	Fields           []cg.FieldInfo
}

type methodData struct {
	MethodName       string
	MethodType       string // "", CREATE, READ, UPDATE, DELETE
	TableName        string
	Ref              string
	SQL              string
	IsExternalSQL    bool
	UseDto           bool
	ReturnedTypeName string
	OutParams        bool
	DtoParam         string
	Fields           []cg.FieldInfo
	Params           []cg.ParamInfo
	Params2          []cg.ParamInfo
	Mappings         []mapping
	Keys             []cg.FieldInfo
}

func (g *DAO) Translate(dao sdmxml.DaoClass) (string, error) {
	g.imports = map[string]importItem{}
	plan, err := cg.PlanDaoMethods(dao.Items, g.dtos, g.core.DtoFieldNamesMode())
	if err != nil {
		return "", err
	}
	var methods []string
	for _, m := range plan {
		text, err := g.render(m)
		if err != nil {
			return "", errors.New(m.ErrorContext + " " + err.Error())
		}
		// the warnings "    // INFO:" are Python comments; any other "//" stays, SQL has them
		methods = append(methods, strings.ReplaceAll(strings.ReplaceAll(text, "\t", "    "), "    // INFO:", "    # INFO:"))
	}
	imports := make([]importItem, 0, len(g.imports))
	for _, it := range g.imports {
		imports = append(imports, it)
	}
	sort.Slice(imports, func(i, j int) bool { return imports[i].File < imports[j].File })
	text, err := render("dao_class", daoData{Imports: imports, ClassName: dao.Name, Methods: methods})
	return strings.ReplaceAll(text, "\r\n", "\n"), err
}

func (g *DAO) render(m cg.DaoMethod) (string, error) {
	switch m.Kind {
	case cg.QueryMethod:
		return g.renderQuery(m)
	case cg.ExecDmlMethod:
		return g.renderExecDml(m)
	}
	if err := checkNoModel(m.DtoClassName); err != nil {
		return "", err
	}
	switch m.Kind {
	case cg.CrudCreate:
		return g.renderCrudCreate(m)
	case cg.CrudRead:
		return g.renderCrudRead(m)
	case cg.CrudUpdate:
		return g.renderCrudUpdate(m)
	case cg.CrudDelete:
		return g.renderCrudDelete(m)
	}
	return "", fmt.Errorf("unexpected kind of DAO method: %d", m.Kind)
}

func warning(msg string) string {
	return "\n    // INFO: " + msg + "\n"
}

func noPKWarning(method string) string {
	return warning(method + " is not rendered because PK is not detected.")
}

func onlyPKWarning(method string) string {
	return warning(method + " is not rendered because all columns are part of PK.")
}

// renderedDtoClassName is the class name without the model; addImport: the DAO file needs it.
func (g *DAO) renderedDtoClassName(dtoClassName string, addImport bool) (string, error) {
	dto, err := cg.FindDto(dtoClassName, g.dtos)
	if err != nil {
		return "", err
	}
	name := cg.ClassNameWithoutModel(dto.Name)
	if addImport {
		file := cg.CamelCaseToLowerSnakeCase(name)
		if g.dtoPackage != "" {
			file = g.dtoPackage + "." + file
		}
		g.imports[name] = importItem{File: file, Class: name}
	}
	return name, nil
}

func (g *DAO) renderQuery(m cg.DaoMethod) (string, error) {
	if m.ReturnTypeIsDto {
		if err := checkNoModel(m.DtoOrReturnType); err != nil {
			return "", err
		}
	}
	// "out_params" is a Python-only extension of the method declaration
	descriptors := m.ParamDescriptors
	outParams := len(descriptors) > 0 && descriptors[len(descriptors)-1] == "out_params"
	if outParams {
		descriptors = descriptors[:len(descriptors)-1]
	}
	q, err := g.core.QueryInfo(m.Ref, descriptors, m.DtoOrReturnType, m.ReturnTypeIsDto, g.dtos)
	if err != nil {
		return "", err
	}
	return g.renderQuerySQL(q, m.ExternalSQL, m.DtoOrReturnType, m.ReturnTypeIsDto, m.FetchList, m.MethodName, "", outParams)
}

// renderQuerySQL is for both <query...> and CRUD read; crudTable == "" means not CRUD.
func (g *DAO) renderQuerySQL(q *cg.DaoSqlInfo, external bool, dtoOrScalar string, isDto, list bool, method, crudTable string, outParams bool) (string, error) {
	if !q.HasSQL {
		return noPKWarning(method), nil
	}
	d := methodData{
		MethodName:    method,
		Ref:           crudTable,
		SQL:           cg.FormatJdbcSQLForPython(q.SQL),
		IsExternalSQL: external,
		UseDto:        isDto,
		OutParams:     outParams,
		Fields:        q.Fields,
		Params:        q.Params,
	}
	if crudTable != "" {
		d.MethodType = "READ"
	}
	switch {
	case isDto:
		// the import is needed only for a list: a single DTO comes as a parameter
		name, err := g.renderedDtoClassName(dtoOrScalar, list)
		if err != nil {
			return "", err
		}
		d.ReturnedTypeName = name
	case len(q.Fields) == 0:
		d.ReturnedTypeName = "?"
	default:
		d.ReturnedTypeName = g.core.TargetTypeByTypeMap(q.Fields[0].Type())
	}
	if list {
		return render("dao_query_list", d)
	}
	return render("dao_query", d)
}

func (g *DAO) renderExecDml(m cg.DaoMethod) (string, error) {
	jdbcSQL, err := cg.JdbcSQLByExecDmlRef(m.Ref, g.core.SQLRoot())
	if err != nil {
		return "", err
	}
	params, err := g.core.ExecDmlParams(jdbcSQL, m.ParamDescriptors)
	if err != nil {
		return "", err
	}
	slots, err := cg.ParseExecDmlParams(m.ParamDescriptors)
	if err != nil {
		return "", err
	}
	d := methodData{
		MethodName:    m.MethodName,
		SQL:           cg.FormatJdbcSQLForPython(jdbcSQL),
		IsExternalSQL: m.ExternalSQL,
	}
	for _, slot := range slots {
		switch slot.Kind {
		case cg.CursorArray:
			var elements []string
			for _, cm := range slot.CursorMappings {
				mp, err := g.createMapping(cm)
				if err != nil {
					return "", err
				}
				d.Mappings = append(d.Mappings, mp)
				d.Params = append(d.Params, cg.NewParamInfo(cg.SnakeCase, "callable", mp.MethodParamName))
				elements = append(elements, mp.ExecDmlParamName)
			}
			d.Params2 = append(d.Params2, cg.NewParamInfo(cg.AsIs, "[]", "["+strings.Join(elements, ", ")+"]"))
		case cg.Mapped:
			if slot.Index >= len(params) { // as in Java: the position must exist among the SQL parameters
				return "", fmt.Errorf("Index %d out of bounds for length %d", slot.Index, len(params))
			}
			mp, err := g.createMapping(slot.Mapping)
			if err != nil {
				return "", err
			}
			d.Mappings = append(d.Mappings, mp)
			d.Params = append(d.Params, cg.NewParamInfo(cg.SnakeCase, "callable", mp.MethodParamName))
			d.Params2 = append(d.Params2, cg.NewParamInfo(cg.SnakeCase, "callable", mp.ExecDmlParamName))
		default:
			p := params[slot.Index]
			d.Params = append(d.Params, p)
			d.Params2 = append(d.Params2, p)
		}
	}
	return render("dao_exec_dml", d)
}

func (g *DAO) createMapping(parts []string) (mapping, error) {
	var m mapping
	declared := strings.TrimSpace(parts[1])
	if err := checkNoModel(declared); err != nil {
		return m, err
	}
	dto, err := cg.FindDto(declared, g.dtos)
	if err != nil {
		return m, err
	}
	m.MethodParamName = strings.TrimSpace(parts[0])
	m.ExecDmlParamName = "_map_cb_" + m.MethodParamName
	if m.DtoClassName, err = g.renderedDtoClassName(declared, true); err != nil {
		return m, err
	}
	if m.Fields, err = g.core.DtoFields(dto); err != nil {
		return m, err
	}
	if len(m.Fields) > 0 {
		m.Fields[0] = m.Fields[0].WithComment(m.Fields[0].Comment() + " [INFO] REF CURSOR")
	}
	return m, nil
}

func (g *DAO) renderCrudCreate(m cg.DaoMethod) (string, error) {
	dto, err := cg.FindDto(m.DtoClassName, g.dtos)
	if err != nil {
		return "", err
	}
	c, err := g.core.CrudCreateInfo(m.TableName, dto, m.AutoColumn)
	if err != nil {
		return "", err
	}
	d := methodData{
		MethodType: "CREATE",
		TableName:  m.TableName,
		SQL:        cg.FormatJdbcSQLForPython(c.SQL),
		MethodName: m.MethodName,
		Params:     c.Params,
	}
	if d.DtoParam, err = g.renderedDtoClassName(m.DtoClassName, false); err != nil { // only for the comments
		return "", err
	}
	if m.FetchGenerated && len(c.Generated) > 0 {
		d.Keys = c.Generated
		return render("dao_create", d)
	}
	return render("dao_exec_dml", d)
}

func (g *DAO) renderCrudRead(m cg.DaoMethod) (string, error) {
	dto, err := cg.FindDto(m.DtoClassName, g.dtos)
	if err != nil {
		return "", err
	}
	c, err := g.core.CrudReadInfo(m.TableName, dto, m.FetchList, m.ExplicitPK)
	if err != nil {
		return "", err
	}
	return g.renderQuerySQL(c, false, m.DtoClassName, true, m.FetchList, m.MethodName, m.TableName, false)
}

func (g *DAO) renderCrudUpdate(m cg.DaoMethod) (string, error) {
	dto, err := cg.FindDto(m.DtoClassName, g.dtos)
	if err != nil {
		return "", err
	}
	c, err := g.core.CrudUpdateInfo(m.TableName, dto, m.ExplicitPK)
	if err != nil {
		return "", err
	}
	if len(c.Params) == 0 {
		return noPKWarning(m.MethodName), nil
	}
	if len(c.Fields) == 0 {
		return onlyPKWarning(m.MethodName), nil
	}
	d := methodData{
		MethodType: "UPDATE",
		MethodName: m.MethodName,
		SQL:        cg.FormatJdbcSQLForPython(c.SQL),
		TableName:  m.TableName,
		Params:     append(cg.ParamsOf(c.Fields), c.Params...),
	}
	if d.DtoParam, err = g.renderedDtoClassName(m.DtoClassName, false); err != nil { // only for the comments
		return "", err
	}
	return render("dao_exec_dml", d)
}

func (g *DAO) renderCrudDelete(m cg.DaoMethod) (string, error) {
	dto, err := cg.FindDto(m.DtoClassName, g.dtos)
	if err != nil {
		return "", err
	}
	c, err := g.core.CrudDeleteInfo(m.TableName, dto, m.ExplicitPK)
	if err != nil {
		return "", err
	}
	if len(c.Params) == 0 {
		return noPKWarning(m.MethodName), nil
	}
	d := methodData{
		MethodType: "DELETE",
		MethodName: m.MethodName,
		SQL:        cg.FormatJdbcSQLForPython(c.SQL),
		TableName:  m.TableName,
		Params:     c.Params,
	}
	return render("dao_exec_dml", d)
}
