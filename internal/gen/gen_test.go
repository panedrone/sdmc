package gen

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/internal/meta/sqlite"
)

// TestGolden is a full cycle on in-memory SQLite: the schema (DDL only, metadata needs no
// data), sdm.xml/settings.xml/SQL of real demo projects, generation, byte-for-byte comparison
// with what the Java plug-in generated on the same projects (sdm-plugins/harness, HarnessMain).
//
// The reference is retaken in sdm-plugins: harness with these projects, then the output
// build/<out>/<label>/ is copied to testdata/golden/<label>/. The templates of sdm-plugins are LF
// (.gitattributes there), as is the output of sdmc.
//
// task_board/refcursors_python, python_branches, cpp, cpp_branches, cpp_camel and cpp_snake are the
// plug-in project of qt6_sqlite3_task_board with this XML (the sdm folder of the Java harness is an absolute path to them).
var goldenCases = []struct {
	label, project, sdm string
}{
	{"go_northwind_no_orm", "northwind", "no_orm/sdm"},
	{"go_northwind_gorm", "northwind", "gorm/sdm"},
	{"go_northwind_sqlx", "northwind", "sqlx/sdm"},
	{"go_log_no_orm", "log", "no_orm/sdm"},
	{"go_log_gorm", "log", "gorm/sdm"},
	{"go_log_sqlx", "log", "sqlx/sdm"},
	{"refcursors_go", "task_board", "refcursors"},
	{"python_thesaurus", "thesaurus", "sdm"},
	{"python_no_orm", "todolist", "sdm"},
	{"refcursors_python", "task_board", "refcursors_python"},
	{"python_branches", "task_board", "python_branches"},
	{"cpp_task_board", "task_board", "cpp"},
	{"cpp_branches", "task_board", "cpp_branches"},
	{"cpp_camel", "task_board", "cpp_camel"},
	{"cpp_snake", "task_board", "cpp_snake"},
}

func TestGolden(t *testing.T) {
	for _, c := range goldenCases {
		t.Run(c.label, func(t *testing.T) {
			project := filepath.Join("testdata", c.project)
			runGolden(t, c.label, project, filepath.Join(project, c.sdm))
		})
	}
}

// runGolden generates on in-memory SQLite and compares with the plug-in reference.
func runGolden(t *testing.T, label, project, sdmDir string) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(project, "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	res, err := Run(filepath.ToSlash(project), sdmDir, db, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "golden", label)
	check(t, filepath.Join(golden, "dto"), res.DtoDir, res.Dto)
	check(t, filepath.Join(golden, "dao"), res.DaoDir, res.Dao)
}

func check(t *testing.T, dir, outputDir string, files []File) {
	t.Helper()
	want := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		want[e.Name()] = true
	}
	expect(t, filepath.Join(dir, "_output_dir.txt"), outputDir)
	delete(want, "_output_dir.txt")
	for _, f := range files {
		if f.Err != nil {
			t.Errorf("%s: %v", f.Class, f.Err)
			continue
		}
		if !want[f.Name] {
			t.Errorf("%s: not in the golden output", f.Name)
			continue
		}
		delete(want, f.Name)
		expect(t, filepath.Join(dir, f.Name), f.Content)
	}
	var missing []string
	for n := range want {
		missing = append(missing, n)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("not generated: %v", missing)
	}
}

func expect(t *testing.T, path, got string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Error(err)
		return
	}
	want := strings.ReplaceAll(string(b), "\r\n", "\n") // a checkout with core.autocrlf=true has CRLF
	if got == want {
		return
	}
	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(gl) || i < len(wl); i++ {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Errorf("%s, line %d:\n got  %q\n want %q", path, i+1, g, w)
			return
		}
	}
}
