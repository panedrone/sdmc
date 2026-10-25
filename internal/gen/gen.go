// Package gen is a generation run for one project: settings.xml and sdm.xml from the sdm folder,
// metadata from the database <jdbc url>, the result is the texts of DTO and DAO files, in Go, Python
// or C++ (see LangOf).
package gen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/panedrone/sdmc/internal/cg"
	"github.com/panedrone/sdmc/internal/cppgen"
	"github.com/panedrone/sdmc/internal/gogen"
	"github.com/panedrone/sdmc/internal/meta"
	"github.com/panedrone/sdmc/internal/meta/mysql"
	"github.com/panedrone/sdmc/internal/meta/pg"
	"github.com/panedrone/sdmc/internal/meta/sqlite"
	"github.com/panedrone/sdmc/internal/pygen"
	"github.com/panedrone/sdmc/internal/sdmxml"
)

// Lang is the target language of a configuration.
type Lang string

const (
	Go     Lang = "go"
	Python Lang = "python"
	Cpp    Lang = "cpp"
)

// Langs are the languages; the first is the default.
var Langs = []Lang{Go, Python, Cpp}

// RootFile is the root file of the IDE plug-in in the sdm folder: go.dal, python.dal, cpp.dal.
func (l Lang) RootFile() string { return string(l) + ".dal" }

// LangOf is the language of the sdm folder by its root file, as in the IDE plug-ins: python.dal is
// Python, cpp.dal is C++. Without them the folder is Go: sdmc never needed go.dal.
func LangOf(sdmDir string) (Lang, error) {
	var found []string
	res := Go
	for _, l := range Langs {
		if exists(filepath.Join(sdmDir, l.RootFile())) {
			found = append(found, l.RootFile())
			res = l
		}
	}
	if len(found) > 1 {
		return "", fmt.Errorf("%s: %s, one folder is one language", sdmDir, strings.Join(found, " and "))
	}
	return res, nil
}

// OneFolder: the models and DAO of the language are in one folder, <folders target>.
func (l Lang) OneFolder() bool { return l == Python || l == Cpp }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// translator is the generator of one language: DTO by class name, DAO by class.
type translator struct {
	dto func(className string) (string, error)
	dao func(dao sdmxml.DaoClass) (string, error)
}

// newTranslator: the Java plug-in has DTO and DAO as two separate runs with their own JdbcUtils.
func newTranslator(lang Lang, md meta.Metadata, s *sdmxml.Settings, sdm *sdmxml.Sdm, sqlRoot string) (*translator, error) {
	if lang == Python {
		// PythonCG: snake_case for fields and parameters, <dto field-names-mode> is ignored
		dtoGen, err := pygen.NewDTO(cg.NewCore(md, s, cg.SnakeCase, cg.SnakeCase, sqlRoot), s, sdm)
		if err != nil {
			return nil, err
		}
		daoGen, err := pygen.NewDAO(cg.NewCore(md, s, cg.SnakeCase, cg.SnakeCase, sqlRoot), s, sdm)
		if err != nil {
			return nil, err
		}
		return &translator{dto: dtoGen.Translate, dao: daoGen.Translate}, nil
	}
	if lang == Cpp {
		// CppCG: <dto field-names-mode> for the fields and the CRUD methods, the parameters as they are
		mode := cppgen.FieldNamesMode(s.Dto.FieldNamesMode)
		dtoGen, err := cppgen.NewDTO(cg.NewCore(md, s, mode, cg.AsIs, sqlRoot), s, sdm)
		if err != nil {
			return nil, err
		}
		daoGen, err := cppgen.NewDAO(cg.NewCore(md, s, mode, cg.AsIs, sqlRoot), s, sdm)
		if err != nil {
			return nil, err
		}
		return &translator{dto: dtoGen.Translate, dao: daoGen.Translate}, nil
	}
	dtoMode := cg.TitleCase // Go naming only, <dto field-names-mode> is ignored
	dtoGen, err := gogen.NewDTO(cg.NewCore(md, s, dtoMode, cg.LowerCamelCase, sqlRoot), s, sdm)
	if err != nil {
		return nil, err
	}
	daoGen, err := gogen.NewDAO(cg.NewCore(md, s, dtoMode, cg.LowerCamelCase, sqlRoot), s, sdm)
	if err != nil {
		return nil, err
	}
	return &translator{dto: dtoGen.Translate, dao: daoGen.Translate}, nil
}

type File struct {
	Class   string
	Name    string // file name
	Content string
	Err     error // the class is not generated
}

type Result struct {
	DtoDir, DaoDir string // relative to the project, as scope in settings.xml
	Dto, Dao       []File
}

// OpenMetadata opens the database of <db url user pwd> (or <jdbc>, for cli/genharness). $PROJECT_DIR$ and ${project_loc} are the project folder,
// a relative SQLite path is relative to it too.
func OpenMetadata(projectDir string, j sdmxml.DB) (meta.Metadata, error) {
	url := strings.NewReplacer("$PROJECT_DIR$", projectDir, "${project_loc}", projectDir).Replace(strings.TrimSpace(j.URL))
	if file, ok := strings.CutPrefix(strings.TrimPrefix(url, "jdbc:"), "sqlite:"); ok {
		if file == ":memory:" {
			return nil, errors.New("sqlite::memory: has no tables to generate from")
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(projectDir, file)
		}
		if _, err := os.Stat(file); err != nil {
			return nil, fmt.Errorf("SQLite database not found: %s (<jdbc url> in settings.xml)", file)
		}
		return sqlite.OpenReadOnly(file)
	}
	if strings.HasPrefix(url, "jdbc:mysql:") || strings.HasPrefix(url, "mysql:") {
		addr, db, u, p, err := mysql.ConnParams(url, j.User, j.Pwd)
		if err != nil {
			return nil, err
		}
		return mysql.Open(addr, db, u, p)
	}
	cs, err := pg.ConnString(url, j.User, j.Pwd)
	if err != nil {
		return nil, errors.New("unsupported database URL (PostgreSQL, MySQL and SQLite only): " + j.URL)
	}
	return pg.Open(context.Background(), cs)
}

// Progress is called before generating each class: n-th of total.
type Progress func(n, total int, class string)

// Run generates the DTO and DAO of the project: all of them or only the classes of only (names, see Select).
// An error of a particular class goes to File.Err, a general error (settings, XML) to error.
// progress may be nil.
func Run(projectDir, sdmDir string, md meta.Metadata, only map[string]bool, progress Progress) (*Result, error) {
	if progress == nil {
		progress = func(int, int, string) {}
	}
	md = meta.Cached(md) // the DTO and DAO passes ask for the same tables again and again
	lang, err := LangOf(sdmDir)
	if err != nil {
		return nil, err
	}
	s, err := sdmxml.LoadSettings(sdmDir)
	if err != nil {
		return nil, err
	}
	sdm, err := sdmxml.LoadSdm(sdmDir)
	if err != nil {
		return nil, err
	}
	res := &Result{}
	if res.DtoDir, res.DaoDir, err = scopes(lang, s); err != nil {
		return nil, err
	}
	picked := func(name string) bool { return only == nil || only[name] }
	tr, err := newTranslator(lang, md, s, sdm, projectDir+"/"+s.Folders.SQL)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, c := range sdm.Classes {
		if picked(c.Name) {
			total++
		}
	}
	for _, c := range sdm.DtoClasses {
		if !picked(c.Name) {
			continue
		}
		progress(len(res.Dto)+1, total, c.Name)
		f := File{Class: c.Name, Name: fileName(lang, c.Name)}
		f.Content, f.Err = tr.dto(c.Name)
		res.Dto = append(res.Dto, f)
	}
	for _, c := range sdm.DaoClasses {
		if !picked(c.Name) {
			continue
		}
		progress(len(res.Dto)+len(res.Dao)+1, total, c.Name)
		f := File{Class: c.Name, Name: fileName(lang, c.Name)}
		f.Content, f.Err = tr.dao(c)
		res.Dao = append(res.Dao, f)
	}
	return res, nil
}

// scopes are the folders of DTO and DAO, relative to the project. Python and C++ have both in
// <folders target>, "" is the project folder.
func scopes(lang Lang, s *sdmxml.Settings) (dto, dao string, err error) {
	if lang.OneFolder() {
		dir := strings.Trim(strings.TrimSpace(strings.ReplaceAll(s.Folders.Target, `\`, "/")), "/")
		return dir, dir, nil
	}
	dto = strings.TrimSpace(strings.ReplaceAll(s.Dto.Scope, `\`, "/"))
	dao = strings.TrimSpace(strings.ReplaceAll(s.Dao.Scope, `\`, "/"))
	if dto == "" || dao == "" {
		return "", "", errors.New(cg.GolangScopesErr)
	}
	return dto, dao, nil
}

// Select returns class names by numbers (from 1, in the order of declaration, as in sdmc ls) and by
// names; one argument may hold several, separated by commas. models/daos: models only
// or DAO only; both or neither mean both kinds. No arguments and no
// models/daos: nil, all of them.
func Select(classes []sdmxml.Class, args []string, models, daos bool) (map[string]bool, error) {
	only, err := byArgs(classes, args)
	if err != nil || models == daos {
		return only, err
	}
	picked := map[string]bool{}
	for _, c := range classes {
		if c.Dao == daos && (only == nil || only[c.Name]) {
			picked[c.Name] = true
		}
	}
	return picked, nil
}

func byArgs(classes []sdmxml.Class, args []string) (map[string]bool, error) {
	var only map[string]bool
	for _, a := range args {
		for _, x := range strings.Split(a, ",") {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			if only == nil {
				only = map[string]bool{}
			}
			if n, err := strconv.Atoi(x); err == nil {
				if n < 1 || n > len(classes) {
					return nil, fmt.Errorf("no class #%d: sdm.xml has %d", n, len(classes))
				}
				only[classes[n-1].Name] = true
				continue
			}
			if !hasClass(classes, x) {
				return nil, fmt.Errorf("no class %q in sdm.xml", x)
			}
			only[x] = true
		}
	}
	return only, nil
}

func hasClass(classes []sdmxml.Class, name string) bool {
	for _, c := range classes {
		if c.Name == name {
			return true
		}
	}
	return false
}

// Entry is a class of sdm.xml and its file.
type Entry struct {
	N int // number, from 1, in the order of declaration
	sdmxml.Class
	Path     string    // relative to the project, with '/'
	Modified time.Time // zero: no file
}

// List returns the classes of sdm.xml in the order of declaration, without the database.
func List(projectDir, sdmDir string) ([]Entry, error) {
	s, err := sdmxml.LoadSettings(sdmDir)
	if err != nil {
		return nil, err
	}
	sdm, err := sdmxml.LoadSdm(sdmDir)
	if err != nil {
		return nil, err
	}
	lang, err := LangOf(sdmDir)
	if err != nil {
		return nil, err
	}
	dtoDir, daoDir, err := scopes(lang, s)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(sdm.Classes))
	for i, c := range sdm.Classes {
		dir := dtoDir
		if c.Dao {
			dir = daoDir
		}
		e := Entry{N: i + 1, Class: c, Path: relPath(dir, fileName(lang, c.Name))}
		if fi, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(e.Path))); err == nil {
			e.Modified = fi.ModTime()
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func relPath(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return strings.TrimSuffix(dir, "/") + "/" + name
}

func fileName(lang Lang, className string) string {
	switch lang {
	case Python:
		return cg.FileNameSnakeCase(cg.ClassNameWithoutModel(className), "py")
	case Cpp:
		return cg.ClassNameWithoutModel(className) + ".h" // as in the plug-in: the class name as is
	}
	return cg.FileNameSnakeCase(cg.ClassNameWithoutModel(className), "go")
}
