package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/internal/meta/sqlite"
)

// sqliteProject is a Go project made by init, with an SQLite database in settings.xml. t.Chdir: no t.Parallel.
func sqliteProject(t *testing.T, ddl string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"init", "-dir", "sdm", "-dao", "internal/dbal", "-orm", "no_orm", "-db", "sqlite"}, &out, &errOut); code != 0 {
		t.Fatalf("init: %d %s", code, errOut.String())
	}
	db, err := sqlite.Open("app.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	if ddl != "" {
		if err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
}

func writeSdm(t *testing.T, xml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join("sdm", "sdm.xml"), []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runCmd(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCommandErrors(t *testing.T) {
	sqliteProject(t, "create table tasks (t_id integer primary key, t_subject text)")
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"bogus"}, 2, `unknown command "bogus"`},
		{[]string{"help"}, 0, ""},
		{[]string{"init", "-dir", "x", "-dao", "y", "-orm", "no_orm", "-db", "sqlite", "extra"}, 2, "unexpected: extra"},
		{[]string{"init", "-nosuch"}, 2, ""},
		{[]string{"gen", "-nosuch"}, 2, ""},
		{[]string{"ds", "TasksDao"}, 2, "datastore takes no classes"},
		{[]string{"ds", "-nosuch"}, 2, ""},
		{[]string{"ls", "-nosuch"}, 2, ""},
		{[]string{"val", "-nosuch"}, 2, ""},
		{[]string{"xml", "-nosuch"}, 2, ""},
		{[]string{"gen", "-dir", "missing"}, 1, ""},
		{[]string{"ls", "-dir", "missing"}, 1, ""},
		{[]string{"xml", "-orm", "no_orm", "-dir", "missing"}, 1, ""},
		{[]string{"gen", "NoSuchClass"}, 2, "NoSuchClass"},
	} {
		code, out, errOut := runCmd(c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d\n%s%s", c.args, code, c.code, out, errOut)
		}
		if c.want != "" && !strings.Contains(out+errOut, c.want) {
			t.Errorf("%v: no %q in\n%s%s", c.args, c.want, out, errOut)
		}
	}
}

// A class that fails is reported, the others are written; val says what is out of date.
func TestGenerateFailedClass(t *testing.T) {
	sqliteProject(t, "create table tasks (t_id integer primary key, t_subject text)")
	writeSdm(t, `<sdm>
    <dto-class name="Task" ref="tasks"/>
    <dto-class name="Bad" ref="no_such_table"/>
    <dao-class name="TasksDao"><crud dto="Task"/></dao-class>
</sdm>`)
	code, out, errOut := runCmd("gen")
	if code != 1 || !strings.Contains(out+errOut, "Bad") || !strings.Contains(out, "created: internal/dbal/tasks_dao.go") {
		t.Errorf("gen: %d\n%s%s", code, out, errOut)
	}
	writeSdm(t, `<sdm>
    <dto-class name="Task" ref="tasks"/>
    <dao-class name="TasksDao"><crud dto="Task"/><query method="count()" ref="select count(*) from tasks"/></dao-class>
</sdm>`)
	if code, out, _ := runCmd("val"); code != 1 || !strings.Contains(out, "out of date: internal/dbal/tasks_dao.go") {
		t.Errorf("val: %d\n%s", code, out)
	}
	// an old XSD is brought up to date by gen
	if err := os.WriteFile(filepath.Join("sdm", "dao.xsd"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCmd("gen"); code != 0 || !strings.Contains(out, "updated: ") {
		t.Errorf("gen: %d\n%s%s", code, out, errOut)
	}
	// a broken sdm.xml
	writeSdm(t, `<sdm><dto-class`)
	if code, _, _ := runCmd("gen"); code != 1 {
		t.Errorf("broken sdm.xml: %d", code)
	}
	if code, _, _ := runCmd("xml", "-orm", "no_orm"); code != 1 {
		t.Errorf("xml, broken sdm.xml: %d", code)
	}
}

func TestXMLEmptyDatabase(t *testing.T) {
	sqliteProject(t, "")
	code, _, errOut := runCmd("xml", "-orm", "no_orm")
	if code != 0 || !strings.Contains(errOut, "no tables and views") {
		t.Errorf("xml: %d %s", code, errOut)
	}
}

func TestPasswordFromEnv(t *testing.T) {
	sqliteProject(t, "create table tasks (t_id integer primary key, t_subject text)")
	writeSdm(t, `<sdm><dto-class name="Task" ref="tasks"/></sdm>`)
	t.Setenv("SDM_PWD", "secret") // SQLite ignores it; the setting is read all the same
	if code, out, errOut := runCmd("gen"); code != 0 {
		t.Errorf("gen: %d\n%s%s", code, out, errOut)
	}
}

func TestDbLabelMore(t *testing.T) {
	for url, want := range map[string]string{
		"sqlserver://h:1433;databaseName=db;encrypt=false": "h:1433/db",
		"jdbc:sqlserver://h:1433;encrypt=false":            "h:1433",
		"sqlserver://h:1433?database=db":                   "h:1433",
		"not a url":                                        "the database",
	} {
		if got := dbLabel(url); got != want {
			t.Errorf("%s: %q, want %q", url, got, want)
		}
	}
}

// red: escape sequences only to a terminal; a file is not one.
func TestRed(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := red(f, "x"); got != "x" {
		t.Errorf("file: %q", got)
	}
	t.Setenv("NO_COLOR", "1")
	if got := red(os.Stdout, "x"); got != "x" {
		t.Errorf("NO_COLOR: %q", got)
	}
	if got := paint("a\n\nb"); got != "\x1b[31ma\x1b[0m\n\n\x1b[31mb\x1b[0m" {
		t.Errorf("paint: %q", got)
	}
}

// A database sdmc cannot open and a broken settings.xml: each command reports it, exit 1.
func TestBadSettings(t *testing.T) {
	sqliteProject(t, "create table tasks (t_id integer primary key, t_subject text)")
	writeSdm(t, `<sdm><dto-class name="Task" ref="tasks"/></sdm>`)
	settings := filepath.Join("sdm", "settings.xml")
	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	set := func(s string) {
		if err := os.WriteFile(settings, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	set(strings.Replace(string(b), `url="sqlite:app.sqlite3"`, `url="sqlite:missing.sqlite3"`, 1))
	for _, args := range [][]string{{"gen"}, {"val"}, {"xml", "-orm", "no_orm"}} {
		if code, out, errOut := runCmd(args...); code != 1 || !strings.Contains(errOut, "missing.sqlite3") {
			t.Errorf("%v, no database: %d\n%s%s", args, code, out, errOut)
		}
	}
	set("<settings>")
	for _, args := range [][]string{{"gen"}, {"xml", "-orm", "no_orm"}, {"ds"}} {
		if code, _, _ := runCmd(args...); code != 1 {
			t.Errorf("%v, broken settings.xml: %d", args, code)
		}
	}
}
