package skeleton

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/datastore"
	"github.com/panedrone/sdmc/internal/sdmxml"
)

func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// configExists reports that the folder already has a configuration (sdm.xml or settings.xml).
func configExists(projectDir, sdmDir string) bool {
	for _, n := range []string{"sdm.xml", "settings.xml"} {
		if _, err := os.Stat(filepath.Join(projectDir, sdmDir, n)); err == nil {
			return true
		}
	}
	return false
}

func TestInit(t *testing.T) {
	dir := newProject(t)
	o := Options{ProjectDir: dir, SdmDir: "go-sdm", DaoDir: "internal/dbal"}
	if configExists(dir, "go-sdm") {
		t.Fatal("Exists before init")
	}
	written, err := Init(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 7 || !configExists(dir, "go-sdm") {
		t.Fatalf("written %d, exists %t", len(written), configExists(dir, "go-sdm"))
	}

	// settings.xml parses and points to where the skeleton was put
	s, err := sdmxml.LoadSettings(filepath.Join(dir, "go-sdm"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Folders.Target != "example.com/app" || s.Folders.SQL != "go-sdm/sql" ||
		s.Dto.Scope != "internal/dbal/models" || s.Dao.Scope != "internal/dbal" {
		t.Errorf("settings: %+v / %+v / %+v", s.Folders, s.Dto, s.Dao)
	}
	if s.Dao.FieldAssignMode != 0 {
		t.Errorf("no_orm: field-assign-mode %d, want 0", s.Dao.FieldAssignMode)
	}
	if _, err := sdmxml.LoadSdm(filepath.Join(dir, "go-sdm")); err != nil {
		t.Error(err)
	}
	ds, err := os.ReadFile(filepath.Join(dir, "internal", "dbal", "data_store_no_orm.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(ds), "package dbal\n") {
		t.Error("data_store_no_orm.go: package dbal, no build constraint expected")
	}

	// -orm sqlx: only the sqlx pair, field-assign-mode="3"
	dir = newProject(t)
	if _, err := Init(Options{ProjectDir: dir, SdmDir: "go-sdm", DaoDir: "store", ORM: "sqlx"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"data_store_sqlx.go", "data_store_sqlx_ex.go"} {
		if b, err := os.ReadFile(filepath.Join(dir, "store", f)); err != nil || !strings.HasPrefix(string(b), "package store\n") {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "store", "data_store_no_orm.go")); err == nil {
		t.Error("data_store_no_orm.go not expected")
	}
	if s, err := sdmxml.LoadSettings(filepath.Join(dir, "go-sdm")); err != nil {
		t.Error(err)
	} else if s.Dao.FieldAssignMode != 3 {
		t.Errorf("sqlx: field-assign-mode %d, want 3", s.Dao.FieldAssignMode)
	}
	if _, err := Init(Options{ProjectDir: newProject(t), SdmDir: "go-sdm", DaoDir: "dbal", ORM: "ent"}); err == nil {
		t.Error("unknown -orm accepted")
	}

	// -db mysql: MySQL keys and url
	dir = newProject(t)
	if _, err := Init(Options{ProjectDir: dir, SdmDir: "go-sdm", DaoDir: "dbal", DB: "mysql"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "go-sdm", "settings.xml")); !strings.Contains(string(b), `detected="gorm-UNSIGNED INT"`) ||
		!strings.Contains(string(b), `url="mysql://`) || strings.Contains(string(b), "{{") {
		t.Errorf("mysql settings.xml:\n%s", b)
	}
	if _, err := Init(Options{ProjectDir: newProject(t), SdmDir: "go-sdm", DaoDir: "dbal", DB: "oracle"}); err == nil {
		t.Error("unknown -db accepted")
	}
}

// A repeated init is an error, and nothing is touched, not even XSD.
func TestInitTwice(t *testing.T) {
	dir := newProject(t)
	o := Options{ProjectDir: dir, SdmDir: "go-sdm", DaoDir: "dbal"}
	if _, err := Init(o); err != nil {
		t.Fatal(err)
	}
	userXML := filepath.Join(dir, "go-sdm", "sdm.xml")
	xsd := filepath.Join(dir, "go-sdm", "sdm.xsd")
	for _, f := range []string{userXML, xsd} {
		if err := os.WriteFile(f, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	written, err := Init(o)
	if err == nil || !strings.Contains(err.Error(), "already exists") || len(written) != 0 {
		t.Fatalf("error expected, got %v (written %d)", err, len(written))
	}
	for _, f := range []string{userXML, xsd} {
		if b, _ := os.ReadFile(f); string(b) != "mine" {
			t.Errorf("%s overwritten", f)
		}
	}
}

// Init with someone else's data store does not create the configuration either: all or nothing.
func TestInitAllOrNothing(t *testing.T) {
	dir := newProject(t)
	ds := filepath.Join(dir, "dbal", "data_store_no_orm.go")
	if err := os.MkdirAll(filepath.Dir(ds), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ds, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(Options{ProjectDir: dir, SdmDir: "go-sdm", DaoDir: "dbal"}); err == nil {
		t.Fatal("error expected")
	}
	if configExists(dir, "go-sdm") {
		t.Error("configuration created in spite of the error")
	}
}

func TestRefreshXSD(t *testing.T) {
	dir := newProject(t)
	if _, err := Init(Options{ProjectDir: dir, SdmDir: "go-sdm", DaoDir: "dbal"}); err != nil {
		t.Fatal(err)
	}
	if res, err := RefreshXSD(dir, "go-sdm"); err != nil || len(res) != 0 {
		t.Fatalf("fresh XSD rewritten: %v %v", res, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go-sdm", "dao.xsd"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := RefreshXSD(dir, "go-sdm"); err != nil || len(res) != 1 {
		t.Fatalf("outdated XSD: %v %v", res, err)
	}
}

func TestInitNoGoMod(t *testing.T) {
	_, err := Init(Options{ProjectDir: t.TempDir(), SdmDir: "go-sdm", DaoDir: "dbal"})
	if err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("go.mod error expected, got %v", err)
	}
}

func TestInitBadPackage(t *testing.T) {
	if _, err := Init(Options{ProjectDir: newProject(t), SdmDir: "go-sdm", DaoDir: "my-dbal"}); err == nil {
		t.Fatal("error expected for 'my-dbal'")
	}
}

func TestRefreshDataStore(t *testing.T) {
	dir := newProject(t)
	if _, err := Init(Options{ProjectDir: dir, SdmDir: "go-sdm", DaoDir: "internal/store", ORM: "sqlx", DB: "mysql"}); err != nil {
		t.Fatal(err)
	}
	ds := filepath.Join(dir, "internal", "store", "data_store_sqlx.go")
	ex := filepath.Join(dir, "internal", "store", "data_store_sqlx_ex.go")
	fresh := DataStoreFile{Path: ds, ORM: "sqlx", DB: "mysql"}
	if res, err := RefreshDataStore(dir, "go-sdm", ""); err != nil || len(res) != 1 || res[0] != fresh {
		t.Fatalf("fresh data store rewritten: %v %v", res, err)
	}
	for f, text := range map[string]string{ds: "package store\n\n// old\n", ex: "package store\n\n// mine\n"} {
		if err := os.WriteFile(f, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	updated := DataStoreFile{Path: ds, ORM: "sqlx", DB: "mysql", Updated: true}
	if res, err := RefreshDataStore(dir, "go-sdm", ""); err != nil || len(res) != 1 || res[0] != updated {
		t.Fatalf("outdated data store: %v %v", res, err)
	}
	want, err := datastore.Files("sqlx", "mysql", "store")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(ds); string(b) != want["data_store_sqlx.go"] {
		t.Error("data_store_sqlx.go: not the mysql/sqlx data store of package store")
	}
	if b, _ := os.ReadFile(ex); string(b) != "package store\n\n// mine\n" {
		t.Error("data_store_sqlx_ex.go touched")
	}
	// -db wins over <db url>
	if res, err := RefreshDataStore(dir, "go-sdm", "pg"); err != nil || len(res) != 1 || res[0].DB != "pg" || !res[0].Updated {
		t.Fatalf("-db pg: %v %v", res, err)
	}
	if err := os.Remove(ds); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshDataStore(dir, "go-sdm", ""); err == nil || !strings.Contains(err.Error(), "sdmc init") {
		t.Fatalf("no data store: %v", err)
	}
}

func TestDbOf(t *testing.T) {
	for url, want := range map[string]string{
		"postgres://localhost:5432/db":   "pg",
		"postgresql://localhost/db":      "pg",
		"jdbc:postgresql://localhost/db": "pg",
		"mysql://localhost:3306/db":      "mysql",
		"jdbc:mysql://localhost/db":      "mysql",
		"sqlite:app.sqlite3":             "sqlite",
		"jdbc:sqlite:$PROJECT_DIR$/a.db": "sqlite",
	} {
		if got, err := dbOf(url); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", url, got, err, want)
		}
	}
	if _, err := dbOf("oracle:thin"); err == nil {
		t.Error("oracle: error expected")
	}
}
