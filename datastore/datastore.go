// Package datastore holds the data store sources the generated DAO rely on:
// one variant per database (pg, mysql, sqlite) and framework (no_orm, sqlx, gorm), datastore/<db>/<orm>/.
// They are in the repository as is, so that they can be downloaded by hand; sdm init takes them from here too.
// Under "//go:build ignore": the go-sdm module needs no drivers, sqlx or gorm.
//
// The variants are built from the shared templates templates/*.go.txt (Generate, cli/dsgen):
// lines between "//sdm:if pg mysql" and "//sdm:end" stay only for the listed databases,
// "//sdm:else" is for the rest. The template is edited, the variants are regenerated; a test makes sure
// they do not diverge.
//
// templates/data_store_no_orm.go.txt comes from src/resources/data_store_no_orm.go of sdm-plugins
// (without Oracle and SQL Server).
//
// Python has no ORM: one variant per database, python/<db>/data_store_no_orm.py, from
// templates/data_store_no_orm.py.txt (src/resources/data_store_no_orm.py of sdm-plugins) with the markers
// "#sdm:if", "#sdm:else", "#sdm:end".
//
// C++ (Qt) has no ORM either: cpp/<db>/DataStore.h and DataStore.cpp, from templates/DataStore.*.txt
// (DataStore_Qt5 of sdm-plugins) with the markers of Go.
package datastore

import (
	"bufio"
	"embed"
	"fmt"
	"go/format"
	"strings"
)

//go:embed templates/*.go.txt templates/*.py.txt templates/DataStore.*.txt pg/*/*.go mysql/*/*.go sqlite/*/*.go python/*/*.py cpp/*/*
var fs embed.FS

// Variants are the values of sdm init -orm, the first is the default.
var Variants = []string{"no_orm", "sqlx", "gorm"}

// DBs are the databases, the first is the default.
var DBs = []string{"pg", "mysql", "sqlite"}

var dbNames = map[string]string{"pg": "PostgreSQL", "mysql": "MySQL", "sqlite": "SQLite"}

var ormNames = map[string]string{"no_orm": "database/sql (no ORM)", "sqlx": "sqlx", "gorm": "gorm"}

func check(variant, db string) error {
	if _, ok := ormNames[variant]; !ok {
		return fmt.Errorf("unknown data store %q, expected one of: %s", variant, strings.Join(Variants, ", "))
	}
	if _, ok := dbNames[db]; !ok {
		return fmt.Errorf("unknown database %q, expected one of: %s", db, strings.Join(DBs, ", "))
	}
	return nil
}

func fileNames(variant string) []string {
	return []string{"data_store_" + variant + ".go", "data_store_" + variant + "_ex.go"}
}

// Files maps a file name to its text for the DAO folder with package pkg: without "//go:build ignore" and the header.
func Files(variant, db, pkg string) (map[string]string, error) {
	if err := check(variant, db); err != nil {
		return nil, err
	}
	res := map[string]string{}
	for _, name := range fileNames(variant) {
		data, err := fs.ReadFile(db + "/" + variant + "/" + name)
		if err != nil {
			return nil, err
		}
		text, ok := withPackage(string(data), pkg)
		if !ok {
			return nil, fmt.Errorf("%s/%s/%s: 'package dbal' not found", db, variant, name)
		}
		res[name] = text
	}
	return res, nil
}

// withPackage drops everything before "package dbal" and renames the package to pkg.
// A checkout with core.autocrlf=true embeds CRLF: the result is LF.
func withPackage(src, pkg string) (string, bool) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	_, body, ok := strings.Cut(src, "\npackage dbal\n")
	if !ok {
		return "", false
	}
	return "package " + pkg + "\n" + body, true
}

// Generate returns the files of variant db/variant from the templates: file name -> text.
func Generate(variant, db string) (map[string][]byte, error) {
	if err := check(variant, db); err != nil {
		return nil, err
	}
	vars := strings.NewReplacer("{{db}}", db, "{{DB}}", dbNames[db], "{{orm}}", variant, "{{ORM}}", ormNames[variant])
	res := map[string][]byte{}
	for _, name := range fileNames(variant) {
		tmpl, err := fs.ReadFile("templates/" + name + ".txt")
		if err != nil {
			return nil, err
		}
		text, err := specialize(string(tmpl), db, "//")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		src, err := format.Source([]byte(vars.Replace(text)))
		if err != nil {
			return nil, fmt.Errorf("%s/%s/%s: %w", db, variant, name, err)
		}
		res[name] = src
	}
	return res, nil
}

// specialize keeps the lines of "//sdm:if ..." blocks for db and removes the markers. Blocks nest.
// comment is the comment of the language: "//" for Go, "#" for Python.
func specialize(tmpl, db, comment string) (string, error) {
	ifMark, elseMark, endMark := comment+"sdm:if ", comment+"sdm:else", comment+"sdm:end"
	var out strings.Builder
	var stack []bool // for each open block: whether its lines are for db
	keep := func() bool {
		for _, k := range stack {
			if !k {
				return false
			}
		}
		return true
	}
	sc := bufio.NewScanner(strings.NewReader(tmpl))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSuffix(sc.Text(), string(rune(13))) // CRLF
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, ifMark):
			match := false
			for _, d := range strings.Fields(strings.TrimPrefix(t, ifMark)) {
				if _, ok := dbNames[d]; !ok {
					return "", fmt.Errorf("line %d: unknown database %q", n, d)
				}
				match = match || d == db
			}
			stack = append(stack, match)
			continue
		case t == elseMark:
			if len(stack) == 0 {
				return "", fmt.Errorf("line %d: %s without %s", n, elseMark, strings.TrimSpace(ifMark))
			}
			stack[len(stack)-1] = !stack[len(stack)-1]
			continue
		case t == endMark:
			if len(stack) == 0 {
				return "", fmt.Errorf("line %d: %s without %s", n, endMark, strings.TrimSpace(ifMark))
			}
			stack = stack[:len(stack)-1]
			continue
		}
		if keep() {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	if len(stack) > 0 {
		return "", fmt.Errorf("%s without %s", strings.TrimSpace(ifMark), endMark)
	}
	return out.String(), sc.Err()
}

// Python ----------------------------------------------------------

// PythonFile is the data store of a Python DAO folder: no ORM, one file, no "_ex".
const PythonFile = "data_store_no_orm.py"

// pyVars are what the template of the Python data store says about each database.
var pyVars = map[string][]string{
	"pg": {"{{driver}}", "psycopg2 or psycopg", "{{import}}", "psycopg2",
		"{{connect}}", "psycopg2.connect(host='localhost', dbname='postgres', user='postgres', password='')",
		"{{out_param_note}}", " and OutParam",
		"{{exec_dml_params}}", "; OutParam for OUT and INOUT of a procedure"},
	"mysql": {"{{driver}}", "mysql-connector-python", "{{import}}", "mysql.connector",
		"{{connect}}", "mysql.connector.connect(host='localhost', database='mysql', user='root', password='')",
		"{{out_param_note}}", " and OutParam",
		"{{exec_dml_params}}", "; OutParam for OUT and INOUT of a procedure, a list of callbacks for its result sets"},
	"sqlite": {"{{driver}}", "sqlite3", "{{import}}", "sqlite3",
		"{{connect}}", "sqlite3.connect('app.sqlite3')",
		"{{out_param_note}}", "",
		"{{exec_dml_params}}", ""},
}

// PythonText is the Python data store of db as it goes to the DAO folder, with LF.
func PythonText(db string) (string, error) {
	if err := check(Variants[0], db); err != nil {
		return "", err
	}
	data, err := fs.ReadFile("python/" + db + "/" + PythonFile)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n"), nil
}

// GeneratePython returns the Python data store of db from its template.
func GeneratePython(db string) ([]byte, error) {
	if err := check(Variants[0], db); err != nil {
		return nil, err
	}
	tmpl, err := fs.ReadFile("templates/" + PythonFile + ".txt")
	if err != nil {
		return nil, err
	}
	text, err := specialize(string(tmpl), db, "#")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", PythonFile, err)
	}
	vars := append([]string{"{{db}}", db, "{{DB}}", dbNames[db]}, pyVars[db]...)
	return []byte(strings.NewReplacer(vars...).Replace(text)), nil
}

// C++ -------------------------------------------------------------

// CppFiles are the data store of a C++ DAO folder: no ORM, no "_ex".
var CppFiles = []string{"DataStore.h", "DataStore.cpp"}

// cppVars are what the templates of the C++ data store say about each database.
var cppVars = map[string][]string{
	"pg": {"{{qt_driver}}", "QPSQL",
		"{{open_params}}", "const QString &host, int port, const QString &database, const QString &user, const QString &password",
		"{{open_args}}", `"localhost", 5432, "postgres", "postgres", ""`,
		"{{open_doc}}", "the connection to the server; yours, as everything but the adapter"},
	"mysql": {"{{qt_driver}}", "QMYSQL",
		"{{open_params}}", "const QString &host, int port, const QString &database, const QString &user, const QString &password",
		"{{open_args}}", `"localhost", 3306, "mysql", "root", ""`,
		"{{open_doc}}", "the connection to the server; yours, as everything but the adapter"},
	"sqlite": {"{{qt_driver}}", "QSQLITE",
		"{{open_params}}", "const QString &file",
		"{{open_args}}", `"app.sqlite3"`,
		"{{open_doc}}", "the SQLite file; yours, as everything but the adapter"},
}

// CppText is the C++ data store of db as it goes to the DAO folder: file name -> text, with LF.
func CppText(db string) (map[string]string, error) {
	if err := check(Variants[0], db); err != nil {
		return nil, err
	}
	res := map[string]string{}
	for _, name := range CppFiles {
		data, err := fs.ReadFile("cpp/" + db + "/" + name)
		if err != nil {
			return nil, err
		}
		res[name] = strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	return res, nil
}

// GenerateCpp returns the C++ data store of db from its templates: file name -> text.
func GenerateCpp(db string) (map[string][]byte, error) {
	if err := check(Variants[0], db); err != nil {
		return nil, err
	}
	vars := strings.NewReplacer(append([]string{"{{db}}", db, "{{DB}}", dbNames[db]}, cppVars[db]...)...)
	res := map[string][]byte{}
	for _, name := range CppFiles {
		tmpl, err := fs.ReadFile("templates/" + name + ".txt")
		if err != nil {
			return nil, err
		}
		text, err := specialize(string(tmpl), db, "//")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		res[name] = []byte(vars.Replace(text))
	}
	return res, nil
}
