// Package cppgen is the C++ generator: the port of CppCG.java and cpp.vm. The DTO and DAO of a C++ project
// are headers <Class>.h in one folder, <folders target> of settings.xml; the names are as in sdm.xml and the
// database (AS_IS).
package cppgen

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/panedrone/go-sdm/internal/cg"
	"github.com/panedrone/go-sdm/internal/sdmxml"
)

//go:embed cpp.tmpl
var cppTmpl string

var tmpl = template.Must(template.New("cpp").Parse(cppTmpl))

func render(name string, data any) (string, error) {
	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func checkSettings(s *sdmxml.Settings) error {
	if strings.TrimSpace(s.Dto.Macro) != "" || strings.TrimSpace(s.Dao.Macro) != "" {
		return errors.New("custom Velocity templates (<dto macro=...>, <dao macro=...>) are not supported by go-sdm")
	}
	return nil
}

// checkNoModel: a model (sqlx-Task) belongs to another language; a C++ class name cannot have '-'.
func checkNoModel(className string) error {
	if m := cg.Model(className); m != "" {
		return fmt.Errorf("%s: the model %q is not for C++, use %s", className, m, cg.ClassNameWithoutModel(className))
	}
	return nil
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
	text, err := render("dto_class", dtoData{ClassName: className, Fields: fields})
	return strings.ReplaceAll(text, "\r\n", "\n"), err
}

// DAO -------------------------------------------------------------

type DAO struct {
	core    *cg.Core
	dtos    []sdmxml.DtoClass
	imports map[string]bool
}

func NewDAO(core *cg.Core, s *sdmxml.Settings, sdm *sdmxml.Sdm) (*DAO, error) {
	if err := checkSettings(s); err != nil {
		return nil, err
	}
	return &DAO{core: core, dtos: sdm.DtoClasses}, nil
}

type daoData struct {
	ClassName string
	Imports   []string
	Methods   []string
}

type methodData struct {
	MethodName       string
	MethodType       string // "", CREATE, UPDATE, DELETE
	TableName        string
	Crud             bool // CRUD read
	Ref              string
	SQL              string
	ExternalSQL      bool
	FetchList        bool
	ReturnTypeIsDto  bool
	ReturnedTypeName string
	DtoParam         string
	Fields           []cg.FieldInfo
	Params           []cg.ParamInfo
	Keys             []cg.FieldInfo
}

func (g *DAO) Translate(dao sdmxml.DaoClass) (string, error) {
	g.imports = map[string]bool{"DataStore.h": true}
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
		methods = append(methods, text)
	}
	imports := make([]string, 0, len(g.imports))
	for imp := range g.imports {
		imports = append(imports, imp)
	}
	sort.Strings(imports)
	text, err := render("dao_class", daoData{ClassName: dao.Name, Imports: imports, Methods: methods})
	return strings.ReplaceAll(text, "\r\n", "\n"), err
}

func (g *DAO) render(m cg.DaoMethod) (string, error) {
	switch m.Kind {
	case cg.QueryMethod:
		return g.renderQuery(m)
	case cg.ExecDmlMethod:
		return g.renderExecDml(m)
	}
	if err := g.processDtoClassName(m.DtoClassName); err != nil {
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

// processDtoClassName: the DAO includes the header of the DTO class.
func (g *DAO) processDtoClassName(dtoClassName string) error {
	if err := checkNoModel(dtoClassName); err != nil {
		return err
	}
	dto, err := cg.FindDto(dtoClassName, g.dtos)
	if err != nil {
		return err
	}
	g.imports[dto.Name+".h"] = true
	return nil
}

func (g *DAO) renderedDtoClassName(dtoClassName string) (string, error) {
	dto, err := cg.FindDto(dtoClassName, g.dtos)
	if err != nil {
		return "", err
	}
	return dto.Name, nil
}

func (g *DAO) renderQuery(m cg.DaoMethod) (string, error) {
	if m.ReturnTypeIsDto {
		if err := g.processDtoClassName(m.DtoOrReturnType); err != nil {
			return "", err
		}
	}
	q, err := g.core.QueryInfo(m.Ref, m.ParamDescriptors, m.DtoOrReturnType, m.ReturnTypeIsDto, g.dtos)
	if err != nil {
		return "", err
	}
	return g.renderQuerySQL(q, m.ExternalSQL, m.DtoOrReturnType, m.ReturnTypeIsDto, m.FetchList, m.MethodName, "", false)
}

// renderQuerySQL is for both <query...> and CRUD read.
func (g *DAO) renderQuerySQL(q *cg.DaoSqlInfo, external bool, dtoOrScalar string, isDto, list bool, method, crudTable string, crud bool) (string, error) {
	if !q.HasSQL {
		return noPKWarning(method), nil
	}
	d := methodData{
		MethodName:      method,
		Crud:            crud,
		Ref:             crudTable,
		SQL:             cg.FormatJdbcSQLForCpp(q.SQL),
		ExternalSQL:     external,
		FetchList:       list,
		ReturnTypeIsDto: isDto,
		Fields:          q.Fields,
		Params:          q.Params,
	}
	if isDto {
		name, err := g.renderedDtoClassName(dtoOrScalar)
		if err != nil {
			return "", err
		}
		d.ReturnedTypeName = name
	} else {
		if len(q.Fields) == 0 {
			return "", errors.New("Index 0 out of bounds for length 0")
		}
		d.ReturnedTypeName = g.core.TargetTypeByTypeMap(q.Fields[0].Type())
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
	d := methodData{
		MethodName:  m.MethodName,
		SQL:         cg.FormatJdbcSQLForCpp(jdbcSQL),
		ExternalSQL: m.ExternalSQL,
		Params:      params,
	}
	return render("dao_exec_dml", d)
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
		SQL:        cg.FormatJdbcSQLForCpp(c.SQL),
		MethodName: m.MethodName,
		Params:     c.Params,
	}
	if d.DtoParam, err = g.renderedDtoClassName(m.DtoClassName); err != nil {
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
	return g.renderQuerySQL(c, false, m.DtoClassName, true, m.FetchList, m.MethodName, m.TableName, true)
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
		SQL:        cg.FormatJdbcSQLForCpp(c.SQL),
		TableName:  m.TableName,
		Params:     append(cg.ParamsOf(c.Fields), c.Params...),
	}
	if d.DtoParam, err = g.renderedDtoClassName(m.DtoClassName); err != nil {
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
		SQL:        cg.FormatJdbcSQLForCpp(c.SQL),
		TableName:  m.TableName,
		Params:     c.Params,
	}
	return render("dao_exec_dml", d)
}
