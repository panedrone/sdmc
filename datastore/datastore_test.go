package datastore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The variants in datastore/<db>/<orm>/ are exactly what is built from the templates (go run ./cli/dsgen).
func TestVariantsUpToDate(t *testing.T) {
	for _, db := range DBs {
		for _, orm := range Variants {
			files, err := Generate(orm, db)
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range files {
				got, err := os.ReadFile(filepath.Join(db, orm, name))
				if err != nil {
					t.Fatal(err)
				}
				if strings.ReplaceAll(string(got), "\r\n", "\n") != string(want) {
					t.Errorf("%s/%s/%s is out of date: go run ./cli/dsgen", db, orm, name)
				}
			}
		}
	}
}

// The sources in a checkout with core.autocrlf=true have CRLF.
func TestWithPackageCRLF(t *testing.T) {
	src := "//go:build ignore\r\n\r\n// header\r\npackage dbal\r\n\r\nfunc f() {}\r\n"
	got, ok := withPackage(src, "store")
	if !ok {
		t.Fatal("'package dbal' not found")
	}
	if want := "package store\n\nfunc f() {}\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// SQLite and MySQL have no PostgreSQL cursor code, SQLite has no procedure code either.
func TestSpecific(t *testing.T) {
	cases := []struct {
		db, orm string
		has     []string
		hasNot  []string
	}{
		{"pg", "no_orm", []string{"RETURNING", "FetchPg", "exec2", "queryImplRc", `sql.Open("pgx"`}, nil},
		{"mysql", "no_orm", []string{"LastInsertId", "queryImplRc", `sql.Open("mysql"`, "type Out", "type InOut", "validateDest"}, []string{"RETURNING", "FetchPg", "exec2"}},
		{"sqlite", "no_orm", []string{"LastInsertId", `sql.Open("sqlite3"`}, []string{"RETURNING", "queryImplRc", "processExecParams", "type Out"}},
		{"sqlite", "gorm", []string{"sqlite.Open"}, []string{"FetchSql", "queryImplRc", "processExecParams"}},
		// the same DAO compile with any of the three: Out/InOut wherever there are procedures
		{"mysql", "gorm", []string{"queryImplRc", "type Out", "type InOut", "validateDest"}, []string{"FetchSql", "exec2"}},
		{"pg", "gorm", []string{"FetchSql", "exec2", "type Out", "type InOut", "validateDest"}, nil},
		{"pg", "sqlx", []string{"Rebind", "RETURNING"}, nil},
		{"mysql", "sqlx", []string{"LastInsertId", "queryImplRc", "type Out", "type InOut", "validateDest"}, []string{"RETURNING", "FetchPg", "exec2"}},
	}
	for _, c := range cases {
		files, err := Files(c.orm, c.db, "dbal")
		if err != nil {
			t.Fatal(err)
		}
		all := ""
		for _, f := range files {
			all += f
		}
		for _, s := range c.has {
			if !strings.Contains(all, s) {
				t.Errorf("%s/%s: %q expected", c.db, c.orm, s)
			}
		}
		for _, s := range c.hasNot {
			if strings.Contains(all, s) {
				t.Errorf("%s/%s: %q not expected", c.db, c.orm, s)
			}
		}
	}
}

// The Python variants in datastore/python/<db>/ are exactly what is built from the template (go run ./cli/dsgen).
func TestPythonVariantsUpToDate(t *testing.T) {
	for _, db := range DBs {
		want, err := GeneratePython(db)
		if err != nil {
			t.Fatal(err)
		}
		got, err := PythonText(db)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("python/%s/%s is out of date: go run ./cli/dsgen", db, PythonFile)
		}
		if strings.Contains(got, "{{") || strings.Contains(got, "#sdm:") {
			t.Errorf("python/%s/%s: a variable or a marker is left", db, PythonFile)
		}
	}
}

// Each Python variant has only what its database can do.
func TestPythonSpecific(t *testing.T) {
	cases := []struct {
		db          string
		has, hasNot []string
	}{
		{"pg", []string{"transaction_status", "RETURNING", "class OutParam", "replace('?', '%s')", "_get_sp_name"}, []string{"lastrowid", "callproc"}},
		{"mysql", []string{"bool(self.conn.in_transaction)", "lastrowid", "class OutParam", "callproc", "stored_results", "replace('?', '%s')"}, []string{"RETURNING"}},
		{"sqlite", []string{"return self.conn.in_transaction", "lastrowid", "execute('begin')"}, []string{"RETURNING", "OutParam", "callproc", "_get_sp_name", "%s"}},
	}
	for _, c := range cases {
		text, err := PythonText(c.db)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range c.has {
			if !strings.Contains(text, s) {
				t.Errorf("%s: %q expected", c.db, s)
			}
		}
		for _, s := range c.hasNot {
			if strings.Contains(text, s) {
				t.Errorf("%s: %q not expected", c.db, s)
			}
		}
	}
}

// The C++ variants in datastore/cpp/<db>/ are exactly what is built from the templates (go run ./cli/dsgen),
// and each has only what its database can do.
func TestCppVariants(t *testing.T) {
	cases := map[string]struct{ has, hasNot []string }{
		"pg":     {[]string{`"QPSQL"`, "select lastval()", "setHostName"}, []string{"query.lastInsertId()"}},
		"mysql":  {[]string{`"QMYSQL"`, "lastInsertId", "setHostName"}, []string{"lastval"}},
		"sqlite": {[]string{`"QSQLITE"`, "lastInsertId", "open(const QString &file)"}, []string{"lastval", "setHostName"}},
	}
	for _, db := range DBs {
		want, err := GenerateCpp(db)
		if err != nil {
			t.Fatal(err)
		}
		got, err := CppText(db)
		if err != nil {
			t.Fatal(err)
		}
		all := ""
		for _, name := range CppFiles {
			if got[name] != string(want[name]) {
				t.Errorf("cpp/%s/%s is out of date: go run ./cli/dsgen", db, name)
			}
			if strings.Contains(got[name], "{{") || strings.Contains(got[name], "//sdm:") {
				t.Errorf("cpp/%s/%s: a variable or a marker is left", db, name)
			}
			all += got[name]
		}
		for _, s := range cases[db].has {
			if !strings.Contains(all, s) {
				t.Errorf("%s: %q expected", db, s)
			}
		}
		for _, s := range cases[db].hasNot {
			if strings.Contains(all, s) {
				t.Errorf("%s: %q not expected", db, s)
			}
		}
	}
}
