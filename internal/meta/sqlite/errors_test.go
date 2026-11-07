package sqlite

import (
	"path/filepath"
	"testing"
)

func TestErrors(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec("create table t (id integer primary key, v decimal(10, 2))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLColumns("  "); err == nil {
		t.Error("empty SQL: no error")
	}
	if _, err := db.SQLColumns("select * from nothing"); err == nil {
		t.Error("no table: no error")
	}
	if _, err := db.Table("a.b.c"); err == nil {
		t.Error("a.b.c: no error")
	}
	if _, err := db.Table("main.t"); err != nil {
		t.Errorf("main.t: %v", err)
	}
	if _, err := db.Table("nothing"); err == nil {
		t.Error("no table: no error")
	}
	if err := db.Exec("not sql"); err == nil {
		t.Error("Exec: no error")
	}
	if _, err := OpenReadOnly(filepath.Join(t.TempDir(), "missing", "x.db")); err == nil {
		t.Error("OpenReadOnly missing: no error")
	}
	for decl, ok := range map[string]bool{"decimal(10, 2)": true, "varchar(x)": false, "decimal(10, y)": false, "varchar(5": false, "int": false} {
		if _, _, got := sizeInParens(decl); got != ok {
			t.Errorf("sizeInParens(%q) = %t", decl, got)
		}
	}
}
