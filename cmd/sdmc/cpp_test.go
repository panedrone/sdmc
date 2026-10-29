package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/internal/meta/sqlite"
)

// C++: init without go.mod, xml, gen, val, ds, .sdm with cpp.dal.
func TestCpp(t *testing.T) {
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	run1 := func(want int, args ...string) string {
		t.Helper()
		out.Reset()
		errOut.Reset()
		if code := run(args, &out, &errOut); code != want {
			t.Fatalf("%v: %d, want %d\n%s%s", args, code, want, out.String(), errOut.String())
		}
		return out.String()
	}
	got := run1(0, "init", "-lang", "cpp", "-dir", "sdm", "-dao", "dal", "-db", "sqlite")
	for _, f := range []string{"sdm/sdm.xml", "sdm/settings.xml", "sdm/cpp.dal", "sdm/sdm.xsd", "dal/DataStore.h", "dal/DataStore.cpp"} {
		if _, err := os.Stat(f); err != nil {
			t.Error(err)
		}
	}
	if !strings.Contains(got, ".sdm (IDE plug-ins, sdmc -c N): the line sdm/cpp.dal") {
		t.Errorf("init: %s", got)
	}
	settings, err := os.ReadFile(filepath.Join("sdm", "settings.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`<folders target="dal" sql="sdm/sql"/>`, `<type-map default="">`, `<type detected="INTEGER" target="CInteger${N}"/>`, `<macro name="${N}">`} {
		if !strings.Contains(string(settings), s) {
			t.Errorf("settings.xml: no %s", s)
		}
	}

	db, err := sqlite.Open("app.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"create table groups (g_id integer primary key autoincrement, g_name text not null)",
		"create table tasks (t_id integer primary key autoincrement, g_id integer references groups(g_id), t_subject text)",
	} {
		if err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	run1(2, "xml", "-dir", "sdm", "-orm", "no_orm")
	if got := run1(0, "xml", "-dir", "sdm", "-d"); !strings.Contains(got,
		`<query-dto-list dto="Task" method="getTasksByGroup(g_id)" ref="tasks(g_id)"/>`) {
		t.Errorf("xml -d:\n%s", got)
	}

	sdmXML := `<sdm>
<dto-class name="Task" ref="tasks"/>
<dao-class name="TasksDao">
    <crud dto="Task"/>
    <query-dto-list dto="Task" method="getTasksByGroup(g_id)" ref="tasks(g_id)"/>
</dao-class>
</sdm>`
	if err := os.WriteFile(filepath.Join("sdm", "sdm.xml"), []byte(sdmXML), 0o644); err != nil {
		t.Fatal(err)
	}
	got = run1(0, "gen", "-dir", "sdm")
	for _, f := range []string{"created: dal/Task.h", "created: dal/TasksDao.h"} {
		if !strings.Contains(got, f) {
			t.Errorf("gen: no %q in\n%s", f, got)
		}
	}
	dto, err := os.ReadFile(filepath.Join("dal", "Task.h"))
	if err != nil {
		t.Fatal(err)
	}
	// the type map of init: CText${N} - a nullable column gets CTextN, the PK does not
	if !strings.Contains(string(dto), "    CInteger t_id; // t\n") || !strings.Contains(string(dto), "    CIntegerN g_id; // t\n") ||
		!strings.Contains(string(dto), "    CTextN t_subject; // t\n") {
		t.Errorf("Task.h:\n%s", dto)
	}
	if got := run1(0, "val", "-dir", "sdm"); !strings.Contains(got, "up to date: 2 classes") {
		t.Errorf("val: %s", got)
	}

	h, cpp := filepath.Join("dal", "DataStore.h"), filepath.Join("dal", "DataStore.cpp")
	if got := run1(0, "ds", "-dir", "sdm"); got != "up to date: "+cpp+" (qt for sqlite)\nup to date: "+h+" (qt for sqlite)\n" {
		t.Errorf("ds: %q", got)
	}
	if err := os.WriteFile(cpp, []byte("// mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run1(0, "ds", "-dir", "sdm"); got != "updated: "+cpp+" (qt for sqlite)\nup to date: "+h+" (qt for sqlite)\n" {
		t.Errorf("ds, changed: %q", got)
	}
	if err := os.Remove(h); err != nil {
		t.Fatal(err)
	}
	run1(1, "ds", "-dir", "sdm")
	if !strings.Contains(errOut.String(), "no DataStore.h in") {
		t.Errorf("ds without DataStore.h: %s", errOut.String())
	}

	if err := os.WriteFile(sdmFile, []byte("sdm/cpp.dal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run1(0, "val"); !strings.Contains(got, "up to date: 2 classes") {
		t.Errorf("val by .sdm: %s", got)
	}
	run1(2, "init", "-lang", "cpp", "-dir", "x", "-dao", "x", "-orm", "no_orm", "-db", "pg")
	if !strings.Contains(errOut.String(), "-orm: not for -lang cpp") {
		t.Errorf("init -lang cpp -orm: %s", errOut.String())
	}
}

// C++ names: a keyword or a method named as its model class is reported by gen, all of them at once,
// not left to the compiler.
func TestCppNames(t *testing.T) {
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	if code := run([]string{"init", "-lang", "cpp", "-dir", "sdm", "-dao", "dal", "-db", "sqlite"}, &out, &errOut); code != 0 {
		t.Fatalf("init: %d %s", code, errOut.String())
	}
	db, err := sqlite.Open("app.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("create table tasks (t_id integer primary key autoincrement, g_id integer, t_subject text)"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	sdmXML := `<sdm>
<dto-class name="Task" ref="tasks"/>
<dao-class name="TasksDao">
    <query-dto-list dto="Task" method="Task(g_id)" ref="tasks(g_id)"/>
    <exec-dml method="delete(t_id)" ref="delete from tasks where t_id=?"/>
    <query method="count_tasks(class)" ref="select count(*) from tasks where g_id=?"/>
    <query-dto-list dto="Task" method="getTasksByGroup(g_id)" ref="tasks(g_id)"/>
    <query-dto-list dto="Task" method="getByGroup(Integer g_id)" ref="tasks(g_id)"/>
    <query method="countAll" ref="select count(*) from tasks" return-type="Long"/>
    <query-dto-list dto="Task" method="getByGroup2(CInteger g_id)" ref="tasks(g_id)"/>
</dao-class>
</sdm>`
	if err := os.WriteFile(filepath.Join("sdm", "sdm.xml"), []byte(sdmXML), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"gen", "-dir", "sdm"}, &out, &errOut); code != 1 {
		t.Fatalf("gen: %d\n%s%s", code, out.String(), errOut.String())
	}
	for _, want := range []string{
		`method "Task" has the name of the class Task`,
		`method "delete" is a C++ keyword`,
		`parameter "class" of count_tasks is a C++ keyword`,
		`parameter g_id: "Integer" is a Java type`,
		`return-type: "Long" is a Java type`,
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("no %q in\n%s", want, errOut.String())
		}
	}
	if strings.Contains(errOut.String(), "getTasksByGroup") || strings.Contains(errOut.String(), "getByGroup2") {
		t.Errorf("a fine method is reported:\n%s", errOut.String())
	}
}

// A scalar query with return-type: the C++ data store reads the column by its name, so the name is the
// one of the query ("count(*)"), not "ret_value" of the plug-in, which gave NULL.
func TestCppScalarColumn(t *testing.T) {
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	if code := run([]string{"init", "-lang", "cpp", "-dir", "sdm", "-dao", "dal", "-db", "sqlite"}, &out, &errOut); code != 0 {
		t.Fatalf("init: %d %s", code, errOut.String())
	}
	db, err := sqlite.Open("app.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("create table tasks (t_id integer primary key autoincrement, t_subject text)"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	sdmXML := `<sdm>
<dao-class name="TasksDao">
    <query method="countTasks" ref="select count(*) from tasks" return-type="java.lang.Integer"/>
    <query method="countTasks2" ref="select count(*) from tasks"/>
</dao-class>
</sdm>`
	if err := os.WriteFile(filepath.Join("sdm", "sdm.xml"), []byte(sdmXML), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"gen", "-dir", "sdm"}, &out, &errOut); code != 0 {
		t.Fatalf("gen: %d %s", code, errOut.String())
	}
	dao, err := os.ReadFile(filepath.Join("dal", "TasksDao.h"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(dao), "ret_value") || strings.Count(string(dao), `DL_STR col_names[] = {DL_T("count(*)")};`) != 2 {
		t.Errorf("TasksDao.h:\n%s", dao)
	}
}
