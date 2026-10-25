package cg

import (
	"errors"
	"strings"

	"github.com/panedrone/sdmc/internal/sdmxml"
)

// The method plan of a DAO class (JaxbUtils.plan_dao_methods): everything that does not depend on the target
// language: method names (including the defaults of an empty <crud/>), parameters, table, PK.

type MethodKind int

const (
	QueryMethod MethodKind = iota
	ExecDmlMethod
	CrudCreate
	CrudRead
	CrudUpdate
	CrudDelete
)

type DaoMethod struct {
	Kind         MethodKind
	ErrorContext string // "<query method="..." ref="..."...\n"
	MethodName   string
	XMLNodeName  string

	// query, exec-dml
	ParamDescriptors []string
	Ref              string
	ExternalSQL      bool
	// query
	ReturnTypeIsDto bool
	DtoOrReturnType string
	FetchList       bool // and CRUD read: "read all" or "read one"

	// CRUD
	DtoClassName   string
	TableName      string
	ExplicitPK     string // "" for "read all"
	AutoColumn     string
	FetchGenerated bool
}

func errorMessage(context string, err error) error {
	return errors.New(context + " " + err.Error())
}

func PlanDaoMethods(items []sdmxml.DaoItem, dtos []sdmxml.DtoClass, mode FieldNamesMode) ([]DaoMethod, error) {
	var res []DaoMethod
	for _, it := range items {
		switch it.Kind() {
		case "query", "query-list", "query-dto", "query-dto-list":
			m, err := planQuery(it)
			if err != nil {
				return nil, err
			}
			res = append(res, m)
		case "exec-dml":
			m, err := planExecDml(it)
			if err != nil {
				return nil, err
			}
			res = append(res, m)
		case "crud":
			crud, err := planCrud(it, dtos, mode)
			if err != nil {
				return nil, err
			}
			res = append(res, crud...)
		default:
			return nil, errors.New("Unexpected element found in DTO XML file")
		}
	}
	return res, nil
}

func planQuery(it sdmxml.DaoItem) (DaoMethod, error) {
	node := it.Kind()
	m := DaoMethod{
		Kind:            QueryMethod,
		XMLNodeName:     node,
		Ref:             it.Ref,
		ExternalSQL:     it.ExternalSQL,
		FetchList:       node == "query-list" || node == "query-dto-list",
		ReturnTypeIsDto: node == "query-dto" || node == "query-dto-list",
		ErrorContext:    "<" + node + " method=\"" + it.Method + "\" ref=\"" + nullable(it.Ref) + "\"...\n",
	}
	if m.ReturnTypeIsDto {
		m.DtoOrReturnType = it.Dto
	} else {
		m.DtoOrReturnType = it.ReturnType
	}
	if err := checkRequiredAttr(node, it.Method); err != nil {
		return m, err
	}
	name, params, err := ParseMethodDeclaration(it.Method)
	if err == nil {
		m.MethodName = name
		m.ParamDescriptors, err = ListedItems(params, false)
	}
	if err != nil {
		return m, errorMessage(m.ErrorContext, err)
	}
	return m, nil
}

func planExecDml(it sdmxml.DaoItem) (DaoMethod, error) {
	node := it.Kind()
	m := DaoMethod{
		Kind:         ExecDmlMethod,
		XMLNodeName:  node,
		Ref:          it.Ref,
		ExternalSQL:  it.ExternalSQL,
		ErrorContext: "<" + node + " method=\"" + it.Method + "\" ref=\"" + nullable(it.Ref) + "\"...\n",
	}
	if err := checkRequiredAttr(node, it.Method); err != nil {
		return m, err
	}
	name, params, err := ParseMethodDeclaration(it.Method)
	if err == nil {
		m.MethodName = name
		m.ParamDescriptors, err = ListedItems(params, true)
	}
	if err != nil {
		return m, errorMessage(m.ErrorContext, err)
	}
	return m, nil
}

// nullable is how Java prints a missing attribute in messages.
func nullable(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

func planCrud(it sdmxml.DaoItem, dtos []sdmxml.DtoClass, mode FieldNamesMode) ([]DaoMethod, error) {
	node := it.Kind()
	dtoName := it.Dto
	if dtoName == "" {
		return nil, errors.New("<" + node + "...\nDTO class is not set")
	}
	errCtx := "<" + node + " dto=\"" + dtoName + "\" table=\"" + it.Table + "\"...\n"
	res, err := func() ([]DaoMethod, error) {
		dto, err := findDto(dtoName, dtos)
		if err != nil {
			return nil, err
		}
		create, readAll, read, update, del := it.Create, it.ReadAll, it.Read, it.Update, it.Delete
		if create == nil && readAll == nil && read == nil && update == nil && del == nil {
			// an empty <crud/>: all five methods with default names
			all := &sdmxml.CrudMethod{}
			create, readAll, read, update, del = all, all, all, all, all
		}
		table, err := refineTableName(dto, it.Table)
		if err != nil {
			return nil, err
		}
		base := DaoMethod{
			ErrorContext:   errCtx,
			XMLNodeName:    node,
			DtoClassName:   dtoName,
			TableName:      table,
			AutoColumn:     dto.Auto,
			FetchGenerated: it.FetchGenerated,
		}
		var res []DaoMethod
		add := func(cm *sdmxml.CrudMethod, kind MethodKind, prefix, suffix, pk string, list bool) {
			if cm == nil {
				return
			}
			m := base
			m.Kind = kind
			m.MethodName = crudMethodName(cm, prefix, dtoName+suffix, mode)
			m.ExplicitPK = pk
			m.FetchList = list
			res = append(res, m)
		}
		add(create, CrudCreate, "create", "", dto.PK, false)
		add(readAll, CrudRead, "read", "List", "", true) // "read all" has no WHERE
		add(read, CrudRead, "read", "", dto.PK, false)
		add(update, CrudUpdate, "update", "", dto.PK, false)
		add(del, CrudDelete, "delete", "", dto.PK, false)
		return res, nil
	}()
	if err != nil {
		return nil, errorMessage(errCtx, err)
	}
	return res, nil
}

func crudMethodName(cm *sdmxml.CrudMethod, base, dtoName string, mode FieldNamesMode) string {
	name := cm.Method
	if name == "" {
		name = base + ClassNameWithoutModel(dtoName)
	}
	return MethodName(name, mode)
}

func refineTableName(dto *sdmxml.DtoClass, table string) (string, error) {
	if table == "" {
		return "", errors.New("'table' is empty")
	}
	if table == "*" {
		if !IsTableRef(dto.Ref) {
			return "", errors.New(", but 'ref' is not a table: <dto-class name=\"" + dto.Name +
				"\" ref=\"" + dto.Ref + "\"..")
		}
		return dto.Ref, nil
	}
	return table, nil
}

// PackageName is the last segment of scope (GoCG._get_package_name).
func PackageName(scope string) (string, error) {
	if strings.TrimSpace(scope) == "" {
		return "", errors.New(GolangScopesErr)
	}
	scope = strings.TrimRight(scope, "/")
	if i := strings.LastIndexByte(scope, '/'); i >= 0 {
		return scope[i+1:], nil
	}
	return scope, nil
}

const GolangScopesErr = "Error in 'settings.xml': \n" +
	"empty values for '<dto scope' and '<dao scope' are not allowed"
