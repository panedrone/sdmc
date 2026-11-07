package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/internal/gen"
	"github.com/panedrone/sdmc/internal/meta"
	"github.com/panedrone/sdmc/internal/meta/sqlite"
	"github.com/panedrone/sdmc/internal/sdmxml"
	"github.com/panedrone/sdmc/internal/skeleton"
)

// The cases below are what users write, right and wrong: a class per case, so that the error of
// one class is checked apart from the others (File.Err). The schema is created in memory; the
// project is made by sdmc init, as a user makes it.

const casesSchema = `
create table groups (
	g_id integer primary key autoincrement,
	g_name varchar(64) not null unique,
	g_comments text
);
create table tasks (
	t_id integer primary key autoincrement,
	g_id integer not null references groups (g_id),
	t_priority integer not null default 1,
	t_subject varchar(256) not null,
	t_date datetime,
	t_amount numeric(10, 2),
	t_blob blob,
	t_flag boolean
);
create index ix_tasks_subject on tasks (t_subject);
create table pairs (a integer not null, b integer not null, v text, primary key (a, b));
create view v_tasks as select t_id, t_subject from tasks;
create table nopk (x integer, y text);
create table onlypk (a integer not null, b integer not null, primary key (a, b));
`

const casesDTO = `
    <dto-class name="Group" ref="groups"/>
    <dto-class name="Task" ref="tasks">
        <field column="t_comments" type="string"/>
        <field column="t_blob" type="-"/>
        <field column="t_priority" type="java.lang.Integer"/>
    </dto-class>
    <dto-class name="Pair" ref="pairs"/>
    <dto-class name="VTask" ref="v_tasks"/>
    <dto-class name="MainTask" ref="main.tasks"/>
    <dto-class name="NoPK" ref="nopk"/>
    <dto-class name="OnlyPK" ref="onlypk"/>
    <dto-class name="TaskAuto" ref="tasks" pk="t_id" auto="t_id"/>
    <dto-class name="PairPK" ref="pairs" pk="a, b"/>
    <dto-class name="BadAuto" ref="tasks" auto="no_col"/>
    <dto-class name="TaskShortcut" ref="tasks(g_id)"/>
    <dto-class name="TaskTyped" ref="tasks">
        <field column="t_subject" type="|0:,omitempty"/>
        <field column="t_date" type="string${json}|0:,omitempty"/>
        <field column="no_such_column" type="java.lang.Integer"/>
    </dto-class>
    <dto-class name="PKDup" ref="pairs" pk="a, a"/>
    <dto-class name="AutoDup" ref="tasks" auto="t_id, t_id"/>
    <dto-class name="TaskLite" ref="select t_id, t_subject, count(*) as n from tasks group by t_id, t_subject"/>
    <dto-class name="TaskFile" ref="tasks.sql"/>
    <dto-class name="TaskNoRef">
        <field column="id" type="int64"/>
        <field column="name" type="string"/>
    </dto-class>
`

// valid DAO: each method kind, shortcuts, files, typed parameters, CRUD with its own names
const casesDAO = `
    <dao-class name="GroupsDao">
        <crud dto="Group" table="groups"/>
        <query method="countGroups()" ref="select count(*) from groups" return-type="int64"/>
        <query-list method="getGroupNames(int64 minID)" ref="select g_name from groups where g_id &gt; ?"/>
        <query-dto method="findGroup(name)" dto="Group" ref="groups(g_name)"/>
        <exec-dml method="renameGroup(name, id)" ref="update groups set g_name = ? where g_id = ?"/>
        <exec-dml method="deleteAll()" ref="delete.sql"/>
    </dao-class>
    <dao-class name="TasksDao">
        <crud dto="Task" table="tasks" fetch-generated="false">
            <create method="addTask"/>
            <read-all method="allTasks"/>
            <read method="taskByID"/>
            <update method="saveTask"/>
            <delete method="dropTask"/>
        </crud>
        <query-dto-list method="getTasksByGroup(gID, priority)" dto="Task" ref="tasks(g_id, t_priority)"/>
        <query-dto-list method="getTaskFiles()" dto="TaskFile"/>
        <query-dto-list method="getLite()" dto="TaskLite"/>
        <query-dto-list method="getByName(name)" dto="TaskNoRef" ref="select t_id as id, t_subject as name from tasks where t_subject = ?"/>
        <query method="maxPriority(gID)" ref="select max(t_priority) from tasks where g_id = ?"/>
    </dao-class>
    <dao-class name="PairsDao">
        <crud dto="Pair">
            <read/>
            <update/>
        </crud>
        <crud dto="VTask">
            <read-all/>
        </crud>
    </dao-class>
    <dao-class name="WarnDao">
        <crud dto="NoPK" table="nopk"><create/><read-all/></crud>
        <crud dto="OnlyPK" table="onlypk"/>
        <crud dto="MainTask" table="main.tasks"/>
        <crud dto="TaskAuto" table="tasks"/>
        <crud dto="PairPK" table="pairs"/>
        <query-dto-list method="getSubjects(gID)" dto="Task" ref="tasks(g_id)/t_id, t_subject"/>
        <query method="subjectOf(id)" ref="tasks(t_id)/t_subject"/>
        <query-list method="subjectsOf(gID)" ref="tasks(g_id) -> t_subject"/>
        <query-list method="escapes()" ref="select 'a&#9;b&#10;c&#13;d\e&quot;f&#8364;' as s"/>
        <query-dto-list method="byShortcutDto(gID)" dto="TaskShortcut"/>
    </dao-class>
`

// one wrong thing per class
var casesErrors = []struct{ class, xml, want string }{
	{"ErrUnknownDto", `<query-dto-list method="m()" dto="Nobody"/>`, "Nobody"},
	{"ErrEmptyRef", `<query method="m()" ref=""/>`, "ref"},
	{"ErrNoMethod", `<query ref="select 1"/>`, "'method' is not set"},
	{"ErrBadDecl", `<query method="m(a" ref="select ?"/>`, "')' expected"},
	{"ErrDupParams", `<query method="m(a, a)" ref="select ? + ?"/>`, "Duplicated parameter names"},
	{"ErrItemName", `<query method="m(1a)" ref="select ?"/>`, "Invalid starting character"},
	{"ErrParamCount", `<query method="m(a)" ref="select ? + ?"/>`, "Parameters declared in method: 1"},
	{"ErrShortcutCount", `<query-dto-list method="m(a)" dto="Task" ref="tasks(g_id, t_priority)"/>`, "Invalid SQL-shortcut"},
	{"ErrShortcutColumn", `<query-dto-list method="m(a)" dto="Task" ref="tasks(no_such)"/>`, "no_such"},
	{"ErrShortcutDml", `<exec-dml method="m(a)" ref="tasks(g_id)"/>`, "SQL-shortcuts are not allowed here"},
	{"ErrNoFile", `<exec-dml method="m()" ref="missing.sql"/>`, "File not found"},
	{"ErrCrudNoDto", `<crud table="tasks"/>`, "DTO class is not set"},
	{"ErrCrudNotTable", `<crud dto="TaskLite"/>`, "is not a table"},
	{"ErrBadSQL", `<query method="m()" ref="select * from no_such_table"/>`, "no_such_table"},
	{"ErrAuto", `<crud dto="BadAuto" table="tasks"/>`, "Unknown columns are listed as 'auto'"},
	{"ErrPKDup", `<crud dto="PKDup" table="pairs"/>`, "Duplicated"},
	{"ErrAutoDup", `<crud dto="AutoDup" table="tasks"/>`, "Duplicated"},
	{"ErrListItem", `<query method="m(a)" ref="select ?" return-type="int64"/><query method="m2([a)" ref="select ?"/>`, "]"},
	{"ErrCursors", `<exec-dml method="m([x])" ref="delete from tasks"/>`, "Implicit cursors are specified incorrectly"},
	{"ErrCrudNoPK", `<crud dto="NoPK" table="nopk"/>`, "PK columns not found"},
	{"ErrBadShortcutRef", `<query-dto-list method="m(a)" dto="Task" ref="tasks(g_id"/>`, "syntax error"},
}

var casesBadDTO = []struct{ class, xml, want string }{
	{"BadFile", `<dto-class name="BadFile" ref="nothing.sql"/>`, "File not found"},
	{"BadTable", `<dto-class name="BadTable" ref="no_such_table"/>`, "no_such_table"},
}

func casesProject(t *testing.T, lang gen.Lang) (dir, sdmDir string) {
	t.Helper()
	dir = t.TempDir()
	o := skeleton.Options{ProjectDir: dir, Lang: lang, SdmDir: "sdm", DB: "sqlite"}
	switch lang {
	case gen.Go:
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		o.DaoDir, o.ORM = "internal/dbal", "no_orm"
	case gen.Python:
		o.DaoDir = "dbal"
	case gen.Cpp:
		o.DaoDir = "dal"
	}
	if _, err := skeleton.Init(o); err != nil {
		t.Fatal(err)
	}
	sdmDir = filepath.Join(dir, "sdm")
	write := func(name, text string) {
		p := filepath.Join(sdmDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sql/tasks.sql", "select t_id, t_subject, t_date from tasks")
	write("sql/delete.sql", "delete from tasks")
	var b strings.Builder
	b.WriteString("<sdm>\n" + casesDTO)
	for _, c := range casesBadDTO {
		b.WriteString("    " + c.xml + "\n")
	}
	b.WriteString(casesDAO)
	for _, c := range casesErrors {
		b.WriteString(`    <dao-class name="` + c.class + `">` + c.xml + "</dao-class>\n")
	}
	b.WriteString("</sdm>\n")
	write("sdm.xml", b.String())
	return dir, sdmDir
}

func runCases(t *testing.T, lang gen.Lang) map[string]gen.File {
	t.Helper()
	dir, sdmDir := casesProject(t, lang)
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec(casesSchema); err != nil {
		t.Fatal(err)
	}
	n := 0
	res, err := gen.Run(filepath.ToSlash(dir), sdmDir, db, nil, func(int, int, string) { n++ })
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]gen.File{}
	for _, f := range append(res.Dto, res.Dao...) {
		files[f.Class] = f
	}
	if n != len(files) {
		t.Errorf("progress called %d times for %d classes", n, len(files))
	}
	return files
}

func TestCases(t *testing.T) {
	for _, lang := range gen.Langs {
		t.Run(string(lang), func(t *testing.T) {
			files := runCases(t, lang)
			for _, name := range []string{"Group", "Task", "Pair", "VTask", "TaskLite", "TaskFile", "TaskNoRef",
				"MainTask", "NoPK", "OnlyPK", "TaskAuto", "PairPK", "TaskShortcut", "TaskTyped", "GroupsDao", "TasksDao", "PairsDao", "WarnDao"} {
				f, ok := files[name]
				switch {
				case !ok:
					t.Errorf("%s: not generated", name)
				case f.Err != nil:
					t.Errorf("%s: %v", name, f.Err)
				case f.Content == "":
					t.Errorf("%s: empty", name)
				}
			}
			for _, c := range append(casesErrors, casesBadDTO...) {
				if c.class == "ErrCursors" && lang == gen.Cpp {
					continue // the C++ generator does not parse implicit cursors
				}
				f := files[c.class]
				if f.Err == nil {
					t.Errorf("%s: no error, want %q", c.class, c.want)
				} else if !strings.Contains(f.Err.Error(), c.want) {
					t.Errorf("%s: %v, want %q", c.class, f.Err, c.want)
				}
			}
			// the names of the XML reach the code in the conventions of the language
			dao := strings.ToLower(files["TasksDao"].Content)
			for _, m := range []string{"addtask", "alltasks", "taskbyid", "savetask", "droptask", "gettasksbygroup", "maxpriority"} {
				if !strings.Contains(strings.ReplaceAll(dao, "_", ""), m) {
					t.Errorf("TasksDao: no %s", m)
				}
			}
			if task := files["Task"].Content; strings.Contains(strings.ToLower(task), "t_blob") || strings.Contains(task, "TBlob") {
				t.Errorf("Task: the excluded field t_blob is generated:\n%s", task)
			}
		})
	}
}

// Select by names and numbers and the -m/-d filters, on the same project.
func TestCasesOnly(t *testing.T) {
	dir, sdmDir := casesProject(t, gen.Go)
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec(casesSchema); err != nil {
		t.Fatal(err)
	}
	res, err := gen.Run(filepath.ToSlash(dir), sdmDir, db, map[string]bool{"Group": true, "GroupsDao": true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Dto) != 1 || len(res.Dao) != 1 || res.Dto[0].Err != nil || res.Dao[0].Err != nil {
		t.Errorf("only: %d %d", len(res.Dto), len(res.Dao))
	}
	for _, lang := range gen.Langs {
		dir, sdmDir := casesProject(t, lang)
		entries, err := gen.List(filepath.ToSlash(dir), sdmDir)
		if err != nil || len(entries) == 0 {
			t.Errorf("%s: List: %d %v", lang, len(entries), err)
		}
	}
	if _, err := gen.List(filepath.ToSlash(dir), filepath.Join(dir, "missing")); err == nil {
		t.Error("List, missing folder: no error")
	}
}

// <custom> and <header> of a Go model: extra fields, an embedded type, comments aligned.
func TestCasesGoCustom(t *testing.T) {
	dir, sdmDir := casesProject(t, gen.Go)
	sdm := "<sdm>\n" + `    <dto-class name="Group" ref="groups">
        <header>// Group is a group of tasks.</header>
        <custom>
{base} BaseModel
GName // the name
Extra string ` + "`json:\"extra\"`" + ` // added
Note int //
        </custom>
    </dto-class>
</sdm>
`
	if err := os.WriteFile(filepath.Join(sdmDir, "sdm.xml"), []byte(sdm), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec(casesSchema); err != nil {
		t.Fatal(err)
	}
	res, err := gen.Run(filepath.ToSlash(dir), sdmDir, db, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Dto) != 1 || res.Dto[0].Err != nil {
		t.Fatalf("%+v", res.Dto)
	}
	got := res.Dto[0].Content
	for _, want := range []string{"// Group is a group of tasks.", "BaseModel", "// the name", "Extra", `json:"extra"`, "// added", "Note"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
}

// OpenMetadata: the SQLite file of <db url>, relative to the project, and the URLs it refuses.
func TestOpenMetadata(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "app.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(casesSchema); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for _, url := range []string{"sqlite:app.sqlite3", "jdbc:sqlite:$PROJECT_DIR$/app.sqlite3", "sqlite:" + filepath.Join(dir, "app.sqlite3")} {
		md, err := gen.OpenMetadata(dir, sdmxml.DB{URL: url})
		if err != nil {
			t.Errorf("%s: %v", url, err)
			continue
		}
		if _, err := md.Table("tasks"); err != nil {
			t.Errorf("%s: %v", url, err)
		}
		md.Close()
	}
	for url, want := range map[string]string{
		"sqlite::memory:":    "has no tables",
		"sqlite:missing.db":  "SQLite database not found",
		"oracle:thin:@x":     "unsupported database URL",
		"mysql://%zz/db":     "",
		"sqlserver://:bad:x": "",
	} {
		_, err := gen.OpenMetadata(dir, sdmxml.DB{URL: url})
		if err == nil {
			t.Errorf("%s: no error", url)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", url, err, want)
		}
	}
}

// What sdmc refuses in settings.xml, for each language: custom Velocity templates; Go: empty scopes.
func TestCasesSettingsErrors(t *testing.T) {
	for _, lang := range gen.Langs {
		dir, sdmDir := casesProject(t, lang)
		settings := filepath.Join(sdmDir, "settings.xml")
		b, err := os.ReadFile(settings)
		if err != nil {
			t.Fatal(err)
		}
		s := strings.Replace(string(b), `macro=""`, `macro="custom.vm"`, 1)
		if err := os.WriteFile(settings, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		db, err := sqlite.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(casesSchema); err != nil {
			t.Fatal(err)
		}
		if _, err := gen.Run(filepath.ToSlash(dir), sdmDir, db, nil, nil); err == nil || !strings.Contains(err.Error(), "Velocity") {
			t.Errorf("%s: %v", lang, err)
		}
		if lang == gen.Go {
			s = strings.Replace(string(b), `<dto scope="internal/dbal/models"`, `<dto scope=""`, 1)
			if err := os.WriteFile(settings, []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := gen.Run(filepath.ToSlash(dir), sdmDir, db, nil, nil); err == nil {
				t.Errorf("go, empty scope: no error")
			}
		}
		db.Close()
	}
}

// A class with an ORM model is Go only: Python and C++ report it as an error of the class.
func TestCasesModelNotGo(t *testing.T) {
	for _, lang := range []gen.Lang{gen.Python, gen.Cpp} {
		dir, sdmDir := casesProject(t, lang)
		if err := os.WriteFile(filepath.Join(sdmDir, "sdm.xml"),
			[]byte(`<sdm><dto-class name="gorm-Task" ref="tasks"/><dao-class name="TasksDao"><crud dto="gorm-Task"/></dao-class></sdm>`), 0o644); err != nil {
			t.Fatal(err)
		}
		db, err := sqlite.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(casesSchema); err != nil {
			t.Fatal(err)
		}
		res, err := gen.Run(filepath.ToSlash(dir), sdmDir, db, nil, nil)
		db.Close()
		if err != nil {
			t.Errorf("%s: %v", lang, err)
			continue
		}
		for _, f := range append(res.Dto, res.Dao...) {
			if f.Err == nil {
				t.Errorf("%s %s: no error", lang, f.Class)
			}
		}
	}
}

// fakeMD is SQLite saying what PostgreSQL says about a function returning a ref-cursor.
type fakeMD struct{ *sqlite.DB }

func (m fakeMD) SQLParams(sql string, typeCount int) (*meta.Params, error) {
	if strings.Contains(sql, "get_tasks_rc(") {
		return &meta.Params{Count: 1, JavaTypes: []string{meta.JavaObject}, RawTypes: []string{""}}, nil
	}
	return m.DB.SQLParams(sql, typeCount)
}

func (m fakeMD) SQLColumns(sql string) ([]meta.Column, error) {
	if strings.Contains(sql, "get_tasks_rc(") {
		return []meta.Column{{Name: "get_tasks_rc", JavaType: "java.sql.ResultSet"}}, nil
	}
	return m.DB.SQLColumns(sql)
}

func TestCasesOtherDrivers(t *testing.T) {
	for _, lang := range gen.Langs {
		dir, sdmDir := casesProject(t, lang)
		sdm := `<sdm>
    <dto-class name="Task" ref="tasks"/>
    <dao-class name="TasksDao">
        <query-dto-list method="byCursor(gID)" dto="Task" ref="select * from get_tasks_rc(?)"/>
    </dao-class>
</sdm>`
		if err := os.WriteFile(filepath.Join(sdmDir, "sdm.xml"), []byte(sdm), 0o644); err != nil {
			t.Fatal(err)
		}
		db, err := sqlite.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(casesSchema); err != nil {
			t.Fatal(err)
		}
		res, err := gen.Run(filepath.ToSlash(dir), sdmDir, fakeMD{db}, nil, nil)
		db.Close()
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		for _, f := range append(res.Dto, res.Dao...) {
			if f.Err != nil {
				t.Errorf("%s %s: %v", lang, f.Class, f.Err)
			}
		}
		dao := strings.ToLower(strings.ReplaceAll(res.Dao[0].Content, "_", ""))
		if !strings.Contains(dao, "bycursor") {
			t.Errorf("%s: methods not generated:\n%s", lang, res.Dao[0].Content)
		}
	}
}
