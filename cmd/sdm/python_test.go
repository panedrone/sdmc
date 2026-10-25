package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panedrone/go-sdm/internal/meta/sqlite"
)

// Python: init without go.mod, xml, gen, val, ls, ds, .sdm with python.dal.
func TestPython(t *testing.T) {
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
	got := run1(0, "init", "-lang", "python", "-dir", "sdm", "-dao", "dbal", "-db", "sqlite")
	for _, f := range []string{"sdm/sdm.xml", "sdm/settings.xml", "sdm/python.dal", "sdm/sdm.xsd", "dbal/data_store_no_orm.py"} {
		if _, err := os.Stat(f); err != nil {
			t.Error(err)
		}
	}
	if !strings.Contains(got, ".sdm (IDE plug-ins, sdm -c N): the line sdm/python.dal") {
		t.Errorf("init: %s", got)
	}
	settings, err := os.ReadFile(filepath.Join("sdm", "settings.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`<folders target="dbal" sql="sdm/sql"/>`, `<db url="sqlite:app.sqlite3"`, `<type detected="INTEGER" target="int"/>`} {
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

	// xml: no -orm, the FK access in snake_case
	run1(2, "xml", "-dir", "sdm", "-orm", "no_orm")
	if got := run1(0, "xml", "-dir", "sdm", "-d"); !strings.Contains(got,
		`<query-dto-list dto="Task" method="get_tasks_by_group(g_id)" ref="tasks(g_id)"/>`) {
		t.Errorf("xml -d:\n%s", got)
	}
	if got := run1(0, "xml", "-dir", "sdm", "-m"); got != "<dto-class name=\"Group\" ref=\"groups\"/>\n<dto-class name=\"Task\" ref=\"tasks\"/>\n" {
		t.Errorf("xml -m: %q", got)
	}

	sdmXML := `<sdm>
<dto-class name="Group" ref="groups"/>
<dto-class name="Task" ref="tasks"/>
<dao-class name="TasksDao">
    <crud dto="Task"/>
    <query-dto-list dto="Task" method="get_tasks_by_group(g_id)" ref="tasks(g_id)"/>
</dao-class>
</sdm>`
	if err := os.WriteFile(filepath.Join("sdm", "sdm.xml"), []byte(sdmXML), 0o644); err != nil {
		t.Fatal(err)
	}
	got = run1(0, "gen", "-dir", "sdm")
	for _, f := range []string{"created: dbal/group.py", "created: dbal/task.py", "created: dbal/tasks_dao.py"} {
		if !strings.Contains(got, f) {
			t.Errorf("gen: no %q in\n%s", f, got)
		}
	}
	dao, err := os.ReadFile(filepath.Join("dbal", "tasks_dao.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"from dbal.task import Task\n", "    def create_task(self, p):\n", "    def get_tasks_by_group(self, g_id):\n",
		`_ai_values = [["t_id", None]]`, "        :param g_id: int\n"} {
		if !strings.Contains(string(dao), s) {
			t.Errorf("tasks_dao.py: no %q in\n%s", s, dao)
		}
	}
	if got := run1(0, "val", "-dir", "sdm"); !strings.Contains(got, "up to date: 3 classes") {
		t.Errorf("val: %s", got)
	}
	if got := run1(0, "ls", "-l", "-dir", "sdm"); !strings.Contains(got, "dbal/tasks_dao.py") {
		t.Errorf("ls -l: %s", got)
	}

	// ds: the data store of the DAO folder, the database by <db url>
	ds := filepath.Join("dbal", "data_store_no_orm.py")
	if got := run1(0, "ds", "-dir", "sdm"); got != "up to date: "+ds+" (no_orm for sqlite)\n" {
		t.Errorf("ds: %q", got)
	}
	if err := os.WriteFile(ds, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run1(0, "ds", "-dir", "sdm"); got != "updated: "+ds+" (no_orm for sqlite)\n" {
		t.Errorf("ds, changed: %q", got)
	}

	// .sdm: the line of python.dal is the configuration
	if err := os.WriteFile(sdmFile, []byte("sdm/python.dal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run1(0, "val"); !strings.Contains(got, "up to date: 3 classes") {
		t.Errorf("val by .sdm: %s", got)
	}

	// a model is an ORM: an error of the class
	sdmXML = `<sdm><dto-class name="sa-Task" ref="tasks"/></sdm>`
	if err := os.WriteFile(filepath.Join("sdm", "sdm.xml"), []byte(sdmXML), 0o644); err != nil {
		t.Fatal(err)
	}
	run1(1, "gen")
	if !strings.Contains(errOut.String(), `sa-Task: the model "sa" is an ORM`) {
		t.Errorf("gen sa-Task: %s", errOut.String())
	}
}

// init -lang python: -orm is an error, the DAO folder is a Python package.
func TestInitPythonErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"init", "-lang", "python", "-dir", "sdm", "-dao", "dbal", "-orm", "no_orm", "-db", "pg"}, 2, "-orm: not for -lang python"},
		{[]string{"init", "-lang", "python", "-dir", "sdm", "-dao", "dbal"}, 2, "missing -db"},
		{[]string{"init", "-lang", "ruby", "-dir", "sdm", "-dao", "dbal", "-db", "pg"}, 2, "-lang ruby: expected go, python"},
		{[]string{"init", "-lang", "python", "-dir", "sdm", "-dao", "my-dbal", "-db", "pg"}, 1, "must be a Python package name"},
	} {
		var out, errOut bytes.Buffer
		if code := run(c.args, &out, &errOut); code != c.code || !strings.Contains(errOut.String(), c.want) {
			t.Errorf("%v: %d %q", c.args, code, errOut.String())
		}
		if entries, _ := os.ReadDir("."); len(entries) != 0 {
			t.Errorf("%v: files created", c.args)
		}
	}
}
