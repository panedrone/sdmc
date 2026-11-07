package skeleton

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/internal/gen"
)

func TestInitErrors(t *testing.T) {
	if _, err := Init(Options{ProjectDir: t.TempDir(), Lang: gen.Python, SdmDir: "sdm", DaoDir: "dbal", ORM: "sqlx"}); err == nil ||
		!strings.Contains(err.Error(), "no ORM") {
		t.Errorf("python -orm: %v", err)
	}
	if _, err := Init(Options{ProjectDir: t.TempDir(), Lang: gen.Cpp, SdmDir: "sdm", DaoDir: "dal", DB: "oracle"}); err == nil ||
		!strings.Contains(err.Error(), "unknown database") {
		t.Errorf("cpp oracle: %v", err)
	}
	// Python without -db: the first database
	dir := t.TempDir()
	if _, err := Init(Options{ProjectDir: dir, Lang: gen.Python, SdmDir: "sdm", DaoDir: "dbal"}); err != nil {
		t.Errorf("python, no -db: %v", err)
	}
	noModule := t.TempDir()
	if err := os.WriteFile(filepath.Join(noModule, "go.mod"), []byte("go 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(Options{ProjectDir: noModule, SdmDir: "sdm", DaoDir: "dbal"}); err == nil ||
		!strings.Contains(err.Error(), "'module' not found") {
		t.Errorf("go.mod without module: %v", err)
	}
}

func TestRefreshDataStoreErrors(t *testing.T) {
	dir := newProject(t)
	if _, err := Init(Options{ProjectDir: dir, SdmDir: "sdm", DaoDir: "dbal", DB: "sqlite"}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "sdm", "settings.xml")
	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	set := func(s string) {
		if err := os.WriteFile(settings, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	set(strings.Replace(string(b), `<db url="sqlite:app.sqlite3"`, `<db url="oracle:thin"`, 1))
	if _, err := RefreshDataStore(dir, "sdm", ""); err == nil {
		t.Error("oracle url: no error")
	}
	set(strings.Replace(string(b), `<db url="sqlite:app.sqlite3" user="" pwd=""/>`, ``, 1))
	if _, err := RefreshDataStore(dir, "sdm", ""); err == nil || !strings.Contains(err.Error(), "no <db url>") {
		t.Errorf("no db: %v", err)
	}
	if _, err := RefreshDataStore(dir, "missing", ""); err == nil {
		t.Error("missing sdm folder: no error")
	}
	if got, err := dbOf("sqlserver://localhost:1433;databaseName=db"); err != nil || got != "mssql" {
		t.Errorf("sqlserver: %q %v", got, err)
	}
}
