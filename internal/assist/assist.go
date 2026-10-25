// Package assist makes the starting XML from the database schema, as the XML assistants of the
// Java plug-in do (SdmUtils.get_crud_sdm_xml, create_crud_xml_jaxb_dao_class, add_fk_access),
// with the defaults of their dialog: no schema, views included, English plural to singular for
// DTO class names, FK access, classes already in use skipped; <crud> is the short one, without
// inner elements (the "crud-auto" choice of the dialog: default method names). Method and
// parameter names follow the language: lower camel case for Go, snake_case for Python, as is for C++.
package assist

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/panedrone/sdmc/internal/cg"
	"github.com/panedrone/sdmc/internal/meta"
)

// DtoClasses returns a <dto-class> for each table and view, except the classes of inUse (names
// without the model: Task, not sqlx-Task). model is the class model of the names: "" for
// database/sql, "sqlx" or "gorm" (sqlx-Task).
func DtoClasses(cat meta.Catalog, model string, inUse map[string]bool) (string, error) {
	tables, err := cat.Tables(true)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, t := range tables {
		name := DtoClassName(t, true)
		if !inUse[name] {
			fmt.Fprintf(&b, "<dto-class name=\"%s\" ref=\"%s\"/>\n", attr(withModel(model, name)), attr(t))
		}
	}
	return b.String(), nil
}

// DaoClasses returns a <dao-class> for each table and view: its <crud>, then <query-dto-list>
// FK access, the DTO classes with the model as in DtoClasses. Skipped are the tables whose DTO
// class has a <crud> already (dtoInUse, names without the model) and those whose DAO class
// name is taken (daoInUse). mode is the naming of the FK access: cg.LowerCamelCase (Go,
// getTasksByGroup(gID)), cg.SnakeCase (Python, get_tasks_by_group(g_id)), cg.AsIs (C++,
// getTasksByGroup(g_id)).
func DaoClasses(cat meta.Catalog, model string, mode cg.FieldNamesMode, dtoInUse, daoInUse map[string]bool) (string, error) {
	tables, err := cat.Tables(true)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, t := range tables {
		dto, dao := DtoClassName(t, true), DaoClassName(t)
		if dtoInUse[dto] || daoInUse[dao] {
			continue
		}
		fmt.Fprintf(&b, "<dao-class name=\"%s\">\n", attr(dao))
		fmt.Fprintf(&b, "    <crud dto=\"%s\" table=\"%s\"/>\n", attr(withModel(model, dto)), attr(t))
		if keys, err := cat.ImportedKeys(t); err == nil {
			fkAccess(&b, model, mode, t, keys)
		} // else as in Java: the <crud> stays, the FK access of the table is skipped
		b.WriteString("</dao-class>\n")
	}
	return b.String(), nil
}

// DaoClassName: the title-cased table name, plural kept, and "Dao": tasks -> TasksDao.
func DaoClassName(table string) string {
	return DtoClassName(table, false) + "Dao"
}

// withModel: "sqlx", "Task" -> "sqlx-Task"; no model, the name as is.
func withModel(model, name string) string {
	if model == "" {
		return name
	}
	return model + "-" + name
}

// fkAccess: a "get<FkTable>By<PkClass>" for each referenced table, all FK columns to it as
// the parameters and the shortcut "fk_table(col, ...)" as the SQL, named by mode (SdmUtils.add_fk_access).
func fkAccess(b *strings.Builder, model string, mode cg.FieldNamesMode, fkTable string, keys []meta.ImportedKey) {
	var pkTables []string
	cols := map[string][]string{}
	for _, k := range keys {
		if _, ok := cols[k.PKTable]; !ok {
			pkTables = append(pkTables, k.PKTable)
		}
		cols[k.PKTable] = append(cols[k.PKTable], k.FKColumn)
	}
	for _, pk := range pkTables {
		params := make([]string, len(cols[pk]))
		for i, c := range cols[pk] {
			switch mode {
			case cg.SnakeCase:
				params[i] = cg.CamelCaseToLowerSnakeCase(c)
			case cg.LowerCamelCase:
				params[i] = cg.LowerCamel(c)
			default:
				params[i] = c
			}
		}
		method := "get" + DtoClassName(fkTable, false) + "By" + DtoClassName(pk, true)
		if mode == cg.SnakeCase {
			method = cg.CamelCaseToLowerSnakeCase(method)
		}
		method += "(" + strings.Join(params, ", ") + ")"
		ref := fkTable + "(" + strings.Join(cols[pk], ", ") + ")"
		fmt.Fprintf(b, "    <query-dto-list dto=\"%s\" method=\"%s\" ref=\"%s\"/>\n",
			attr(withModel(model, DtoClassName(fkTable, true))), attr(method), attr(ref))
	}
}

// DtoClassName is table_name_to_dto_class_name: the title-cased table name, with the last
// word made singular if singular.
func DtoClassName(table string, singular bool) string {
	word := cg.Title(table)
	if !singular {
		return word
	}
	r := []rune(word)
	last := -1
	for i := len(r) - 1; i >= 0; i-- {
		if unicode.IsUpper(r[i]) {
			last = i
			break
		}
	}
	if last < 0 {
		return word // Java throws here; the name is left as is
	}
	w := []rune(singularOf(string(r[last:])))
	if len(w) == 0 {
		return word
	}
	return string(r[:last]) + string(unicode.ToUpper(w[0])) + strings.ToLower(string(w[1:]))
}

var attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func attr(s string) string {
	return attrEscaper.Replace(s)
}
