package gogen

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

//go:embed go.tmpl
var goTmpl string

var tmpl = template.Must(template.New("go").Parse(goTmpl))

func render(name string, data any) (string, error) {
	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Settings is what the generator needs from settings.xml.
func checkNoVelocity(s *sdmxml.Settings) error {
	if strings.TrimSpace(s.Dto.Macro) != "" || strings.TrimSpace(s.Dao.Macro) != "" {
		return errors.New("custom Velocity templates (<dto macro=...>, <dao macro=...>) are not supported by sdmc")
	}
	return nil
}

// DTO -------------------------------------------------------------

type DTO struct {
	core *cg.Core
	pkg  string
	dtos []sdmxml.DtoClass
}

func NewDTO(core *cg.Core, s *sdmxml.Settings, sdm *sdmxml.Sdm) (*DTO, error) {
	if err := checkNoVelocity(s); err != nil {
		return nil, err
	}
	pkg, err := cg.PackageName(strings.TrimSpace(strings.ReplaceAll(s.Dto.Scope, `\`, "/")))
	if err != nil {
		return nil, err
	}
	return &DTO{core: core, pkg: pkg, dtos: sdm.DtoClasses}, nil
}

type dtoData struct {
	Package   string
	Imports   []string
	HasHeader bool
	Header    string
	ClassName string
	Fields    []string
}

func (g *DTO) Translate(className string) (string, error) {
	dto, err := cg.FindDto(className, g.dtos)
	if err != nil {
		return "", err
	}
	fields, err := g.core.DtoFields(dto)
	if err != nil {
		return "", err
	}
	formatted, fields, err := formatFields(dto, fields) // before imports: <custom> adds fields
	if err != nil {
		return "", err
	}
	imports := map[string]bool{}
	for _, fi := range fields {
		if imp := typeImport(fi.Type()); imp != "" {
			imports[imp] = true
		}
	}
	d := dtoData{
		Package:   g.pkg,
		Imports:   sorted(imports),
		ClassName: cg.ClassNameWithoutModel(className),
		Fields:    formatted,
	}
	if dto.Header != nil {
		d.HasHeader, d.Header = true, *dto.Header
	}
	text, err := render("dto_class", d)
	return strings.ReplaceAll(text, "\r\n", "\n"), err
}

func sorted(set map[string]bool) []string {
	res := make([]string, 0, len(set))
	for s := range set {
		res = append(res, s)
	}
	sort.Strings(res)
	return res
}

// DAO -------------------------------------------------------------

type DAO struct {
	core       *cg.Core
	settings   *sdmxml.Settings
	dtos       []sdmxml.DtoClass
	dtoPackage string
	daoPackage string
	imports    map[string]bool
}

func NewDAO(core *cg.Core, s *sdmxml.Settings, sdm *sdmxml.Sdm) (*DAO, error) {
	if err := checkNoVelocity(s); err != nil {
		return nil, err
	}
	dtoPkg, err := cg.PackageName(strings.TrimSpace(strings.ReplaceAll(s.Dto.Scope, `\`, "/")))
	if err != nil {
		return nil, err
	}
	daoPkg, err := cg.PackageName(strings.TrimSpace(strings.ReplaceAll(s.Dao.Scope, `\`, "/")))
	if err != nil {
		return nil, err
	}
	return &DAO{core: core, settings: s, dtos: sdm.DtoClasses, dtoPackage: dtoPkg, daoPackage: daoPkg}, nil
}

type daoData struct {
	Package string
	Imports []string
	Methods []string
}

type mapping struct {
	MethodParamName  string
	ExecDmlParamName string
	DtoClassName     string
	Fields           []cg.FieldInfo
}

type methodData struct {
	MethodName       string
	Model            string
	TableName        string
	Ref              string
	MethodType       string
	SQL              string
	IsExternalSQL    bool
	ReturnedTypeName string
	AssignMode       int
	Fields           []cg.FieldInfo
	Params           []cg.ParamInfo
	Params2          []cg.ParamInfo
	Mappings         []mapping
	DtoParam         string
	AiNames          string
	FieldsAI         []cg.FieldInfo
}

func (g *DAO) Translate(dao sdmxml.DaoClass) (string, error) {
	g.imports = map[string]bool{}
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
		methods = append(methods, strings.ReplaceAll(text, "    //", "//"))
	}
	g.imports["context"] = true
	text, err := render("dao_class", daoData{Package: g.daoPackage, Imports: sorted(g.imports), Methods: methods})
	return strings.ReplaceAll(text, "\r\n", "\n"), err
}

func (g *DAO) render(m cg.DaoMethod) (string, error) {
	switch m.Kind {
	case cg.QueryMethod:
		return g.renderQuery(m)
	case cg.ExecDmlMethod:
		return g.renderExecDml(m)
	case cg.CrudCreate:
		g.processDtoClassName(m.DtoClassName)
		return g.renderCrudCreate(m)
	case cg.CrudRead:
		g.processDtoClassName(m.DtoClassName)
		return g.renderCrudRead(m)
	case cg.CrudUpdate:
		g.processDtoClassName(m.DtoClassName)
		return g.renderCrudUpdate(m)
	case cg.CrudDelete:
		g.processDtoClassName(m.DtoClassName)
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

func (g *DAO) refineMethodName(name string) string {
	return cg.MethodName(name, g.core.DtoFieldNamesMode())
}

func (g *DAO) renderedDtoClassName(dtoClassName string) (string, error) {
	dto, err := cg.FindDto(dtoClassName, g.dtos)
	if err != nil {
		return "", err
	}
	name := cg.ClassNameWithoutModel(dto.Name)
	if g.dtoPackage == g.daoPackage {
		return name, nil
	}
	return g.dtoPackage + "." + name, nil
}

func (g *DAO) processDtoClassName(string) {
	if g.dtoPackage == g.daoPackage {
		return
	}
	imp := g.settings.Dto.Scope
	if module := g.settings.Folders.Target; strings.TrimSpace(module) != "" {
		imp = module + "/" + imp
	}
	g.imports[imp] = true
}

// stripImportsAndTags: parameter types are rendered without imports and tags, imports go to 'imports'.
func (g *DAO) stripImportsAndTags(params []cg.ParamInfo) {
	for i, p := range params {
		if imp := typeImport(p.Type()); imp != "" {
			g.imports[imp] = true
		}
		params[i] = p.WithType(typeWithoutImportAndTag(p.Type()))
	}
}

func (g *DAO) assignParams(params []cg.ParamInfo, dtoParamType string, d *methodData) error {
	if dtoParamType != "" {
		if len(params) == 0 {
			return errors.New("DTO parameter specified but SQL-query does not contain any parameters")
		}
		g.processDtoClassName(dtoParamType)
		name, err := g.renderedDtoClassName(dtoParamType)
		if err != nil {
			return err
		}
		d.DtoParam = name
	} else {
		g.stripImportsAndTags(params)
	}
	d.Params = params
	return nil
}

func (g *DAO) renderQuery(m cg.DaoMethod) (string, error) {
	if m.ReturnTypeIsDto {
		g.processDtoClassName(m.DtoOrReturnType)
	}
	q, err := g.core.QueryInfo(m.Ref, m.ParamDescriptors, m.DtoOrReturnType, m.ReturnTypeIsDto, g.dtos)
	if err != nil {
		return "", err
	}
	return g.renderQuerySQL(q, m.ExternalSQL, m.DtoOrReturnType, m.ReturnTypeIsDto, m.FetchList, m.MethodName, "")
}

// renderQuerySQL is for both <query...> and CRUD read.
func (g *DAO) renderQuerySQL(q *cg.DaoSqlInfo, external bool, dtoOrScalar string, isDto, list bool, method, crudTable string) (string, error) {
	if !q.HasSQL {
		return noPKWarning(method), nil
	}
	d := methodData{
		SQL:           cg.FormatJdbcSQLForGo(q.SQL),
		IsExternalSQL: external,
		AssignMode:    g.settings.Dao.FieldAssignMode,
		MethodName:    g.refineMethodName(method),
		TableName:     crudTable,
		Ref:           crudTable,
	}
	if crudTable != "" {
		d.MethodType = "READ"
	}
	fields := q.Fields
	if isDto {
		name, err := g.renderedDtoClassName(dtoOrScalar)
		if err != nil {
			return "", err
		}
		d.ReturnedTypeName = name
		d.Model = cg.Model(dtoOrScalar)
	} else {
		ret := fields[0]
		if imp := typeImport(ret.Type()); imp != "" {
			g.imports[imp] = true
		}
		d.ReturnedTypeName = typeWithoutImportAndTag(ret.Type())
		fields[0] = ret.WithRenderedType(d.ReturnedTypeName)
	}
	d.Fields = fields
	if err := g.assignParams(q.Params, "", &d); err != nil {
		return "", err
	}
	mode := "dao_query"
	switch {
	case list && isDto:
		mode = "dao_query_all_dto"
	case list:
		mode = "dao_query_all"
	case isDto:
		mode = "dao_query_dto"
	}
	return render(mode, d)
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
	g.stripImportsAndTags(params)
	slots, err := cg.ParseExecDmlParams(m.ParamDescriptors)
	if err != nil {
		return "", err
	}
	d := methodData{
		MethodName:    g.refineMethodName(m.MethodName),
		SQL:           cg.FormatJdbcSQLForGo(jdbcSQL),
		IsExternalSQL: m.ExternalSQL,
		AssignMode:    g.settings.Dao.FieldAssignMode,
	}
	var methodParams []cg.ParamInfo
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
				methodParams = append(methodParams, cg.NewParamInfo(cg.LowerCamelCase, "func(*"+mp.DtoClassName+")", mp.MethodParamName))
				elements = append(elements, mp.ExecDmlParamName)
			}
			// no "(Object) " cast: a Go variadic parameter takes a slice as one argument anyway
			d.Params2 = append(d.Params2, cg.NewParamInfo(cg.AsIs, "[]", "[]any{"+strings.Join(elements, ", ")+"}"))
		case cg.Mapped:
			p := params[slot.Index]
			mp, err := g.createMapping(slot.Mapping)
			if err != nil {
				return "", err
			}
			d.Mappings = append(d.Mappings, mp)
			methodParams = append(methodParams, cg.NewParamInfo(cg.LowerCamelCase, "func(*"+mp.DtoClassName+")", mp.MethodParamName))
			d.Params2 = append(d.Params2, cg.NewParamInfo(cg.AsIs, g.core.TargetTypeByTypeMap(p.Type()), mp.ExecDmlParamName))
		default:
			p := params[slot.Index]
			methodParams = append(methodParams, p)
			d.Params2 = append(d.Params2, p)
		}
	}
	if err := g.assignParams(methodParams, "", &d); err != nil {
		return "", err
	}
	return render("dao_exec_dml", d)
}

func (g *DAO) createMapping(parts []string) (mapping, error) {
	var m mapping
	m.MethodParamName = cg.LowerCamel(strings.TrimSpace(parts[0]))
	m.ExecDmlParamName = m.MethodParamName + "Mapper"
	declared := strings.TrimSpace(parts[1])
	dto, err := cg.FindDto(declared, g.dtos)
	if err != nil {
		return m, err
	}
	g.processDtoClassName(dto.Name)
	if m.Fields, err = g.core.DtoFields(dto); err != nil {
		return m, err
	}
	m.DtoClassName, err = g.renderedDtoClassName(declared)
	return m, err
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
		Model:      cg.Model(m.DtoClassName),
		SQL:        cg.FormatJdbcSQLForGo(c.SQL),
		MethodName: m.MethodName,
		Params:     c.Params,
	}
	if d.DtoParam, err = g.renderedDtoClassName(m.DtoClassName); err != nil {
		return "", err
	}
	if m.FetchGenerated && len(c.Generated) > 0 {
		var names []string
		for _, f := range c.Generated {
			names = append(names, f.ColumnName())
		}
		d.AiNames = strings.Join(names, ",")
		d.FieldsAI = c.Generated
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
	return g.renderQuerySQL(c, false, m.DtoClassName, true, m.FetchList, m.MethodName, m.TableName)
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
		Model:      cg.Model(m.DtoClassName),
		TableName:  m.TableName,
		MethodName: m.MethodName,
		SQL:        cg.FormatJdbcSQLForGo(c.SQL),
	}
	params := append(cg.ParamsOf(c.Fields), c.Params...)
	if err := g.assignParams(params, m.DtoClassName, &d); err != nil {
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
		TableName:  m.TableName,
		MethodName: m.MethodName,
		SQL:        cg.FormatJdbcSQLForGo(c.SQL),
		Model:      cg.Model(m.DtoClassName),
	}
	if err := g.assignParams(c.Params, m.DtoClassName, &d); err != nil {
		return "", err
	}
	return render("dao_exec_dml", d)
}
