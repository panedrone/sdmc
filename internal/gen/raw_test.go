package gen

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/internal/meta/sqlite"
)

// Raw driver names in type-map are on a par with the Java names of the plug-in.

const rawSchema = `create table tasks (t_id integer primary key autoincrement, t_subject varchar(64) not null,
	t_rating real, t_data blob, t_date datetime);`

const rawSdm = `<sdm>
	<dto-class name="Task" ref="tasks"/>
	<dto-class name="TaskCount" ref="select count(*) as cnt, max(t_subject) as last_subject from tasks"/>
	<dao-class name="TasksDao">
		<crud dto="Task"/>
		<query-dto-list dto="Task" method="FindTasks(t_subject)" ref="select * from tasks where t_subject like ?"/>
		<query method="CountTasks" ref="select count(*) from tasks" return-type="java.lang.Long"/>
	</dao-class>
</sdm>`

func rawSettings(typeMap string) string {
	return `<settings>
	<folders target="example.com/app" sql="sql"/>
	<dto scope="dbal/models" inheritance="" field-names-mode="0" macro=""/>
	<dao scope="dbal" field-assign-mode="0" macro=""/>
	<jdbc url="jdbc:sqlite:x" user="" pwd=""/>
	<type-map default="">` + typeMap + `</type-map>
	<macros>
		<macro name="${json}" value=" ` + "`json:&quot;${column}&quot;`" + `"/>
	</macros>
</settings>`
}

func generateRaw(t *testing.T, typeMap string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range map[string]string{"sdm.xml": rawSdm, "settings.xml": rawSettings(typeMap)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec(rawSchema); err != nil {
		t.Fatal(err)
	}
	res, err := Run(dir, dir, db, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range append(res.Dto, res.Dao...) {
		if f.Err != nil {
			t.Fatalf("%s: %v", f.Class, f.Err)
		}
		out[f.Name] = f.Content
	}
	return out
}

// The same targets under Java keys and under raw SQLite keys give the same code byte for byte.
func TestRawKeysSameAsJava(t *testing.T) {
	java := generateRaw(t, `
		<type detected="java.lang.Integer" target="int64${json} -> SetInt64"/>
		<type detected="java.lang.String" target="string${json} -> SetString"/>
		<type detected="java.lang.Float" target="float64${json} -> SetFloat64"/>
		<type detected="byte[]" target="[]byte${json} -> SetBytes"/>
		<type detected="java.lang.Long" target="int64${json} -> SetInt64"/>
		<type detected="java.lang.Object" target="any${json}"/>`)
	raw := generateRaw(t, `
		<type detected="INTEGER" target="int64${json} -> SetInt64"/>
		<type detected="VARCHAR" target="string${json} -> SetString"/>
		<type detected="DATETIME" target="string${json} -> SetString"/>
		<type detected="REAL" target="float64${json} -> SetFloat64"/>
		<type detected="BLOB" target="string${json} -> SetString"/> <!-- xerial (the plug-in) sees BLOB as VARCHAR -->
		<type detected="java.lang.String" target="string${json} -> SetString"/> <!-- parameters: SQLite has no raw type -->
		<type detected="java.lang.Long" target="int64${json} -> SetInt64"/>
		<type detected="unknown" target="any${json}"/>`)
	if len(java) == 0 || len(java) != len(raw) {
		t.Fatalf("files: %d vs %d", len(java), len(raw))
	}
	for name, want := range java {
		if raw[name] != want {
			t.Errorf("%s differs:\n--- java keys\n%s\n--- raw keys\n%s", name, want, raw[name])
		}
	}
}

// An empty type-map gives raw driver names in the code: it shows which keys are needed.
func TestEmptyTypeMapRendersRaw(t *testing.T) {
	out := generateRaw(t, "")
	task := out["task.go"]
	for _, want := range []string{`TID\s+INTEGER\n`, `TSubject\s+VARCHAR\n`, `TRating\s+REAL\n`, `TData\s+BLOB\n`, `TDate\s+DATETIME\n`} {
		if !regexp.MustCompile(want).MatchString(task) {
			t.Errorf("task.go: %q expected:\n%s", want, task)
		}
	}
	// SQLite reports no type for expressions: the key is unknown
	if c := out["task_count.go"]; !strings.Contains(c, "Cnt         unknown") {
		t.Errorf("task_count.go: unknown expected:\n%s", c)
	}
}
