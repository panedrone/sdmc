package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGoDals(t *testing.T) {
	text := "# app\r\n\r\n--- sdm\r\n\r\ngo-sdm/go.dal\r\n\r\n# sdm/go.dal\r\n" +
		"internal/gorm/sdm/go.dal | gorm\r\n./internal/sqlx/sdm/go.dal\r\n\r\n---\r\n" +
		"internal/sdm/java.dal\r\nalgo.dal\r\nREADME.md\r\n"
	want := []string{"go-sdm/go.dal", "internal/gorm/sdm/go.dal", "internal/sqlx/sdm/go.dal"}
	if got := dalFiles(text); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := dalFiles(""); got != nil {
		t.Errorf("empty: %q", got)
	}
}

// configDirs with the flags of a command: args as on the command line, typed as "sdmc gen";
// the folders are joined by commas.
func resolve(t *testing.T, sdm string, args ...string) (string, string, error) {
	t.Helper()
	if sdm != "" {
		if err := os.WriteFile(sdmFile, []byte(sdm), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		_ = os.Remove(sdmFile)
	}
	fs, dir := newFlags("test", io.Discard)
	c := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	cfgs, err := configDirs(fs, *dir, *c, "sdmc gen", &errOut)
	var dirs []string
	for _, c := range cfgs {
		dirs = append(dirs, c.dir)
	}
	return strings.Join(dirs, ","), errOut.String(), err
}

// replaces the console; no t.Parallel: package variables.
func console(t *testing.T, terminal bool, input string) {
	t.Helper()
	savedIn, savedTerm := stdin, stdinIsTerminal
	stdin = strings.NewReader(input)
	stdinIsTerminal = func() bool { return terminal }
	t.Cleanup(func() { stdin, stdinIsTerminal = savedIn, savedTerm })
}

// squeeze makes the columns of a table single-spaced.
func squeeze(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// hasTable: the table of configurations with the ready commands.
func hasTable(out string) bool {
	return strings.Contains(squeeze(out), "# configuration command "+
		"1 a/go.dal sdmc gen -c 1 "+
		"2 b/sdm/go.dal sdmc gen -c 2 "+
		"all (one after another) sdmc gen -c all")
}

func TestConfigDir(t *testing.T) {
	t.Chdir(t.TempDir())
	console(t, false, "")
	two := "--- sdm\na/go.dal\n\nb/sdm/go.dal|b\n"

	if d, _, err := resolve(t, ""); err != nil || d != "sdm" {
		t.Errorf("no .sdm: %q %v", d, err)
	}
	if d, _, err := resolve(t, "# only comments\nREADME.md\n"); err != nil || d != "sdm" {
		t.Errorf("no go.dal: %q %v", d, err)
	}
	if d, _, err := resolve(t, "x/go.dal\n"); err != nil || d != "x" {
		t.Errorf("the only one: %q %v", d, err)
	}
	if d, _, err := resolve(t, two, "-c", "2"); err != nil || d != "b/sdm" {
		t.Errorf("-c 2: %q %v", d, err)
	}
	if d, _, err := resolve(t, two, "-c", "all"); err != nil || d != "a,b/sdm" {
		t.Errorf("-c all: %q %v", d, err)
	}
	if d, _, err := resolve(t, "x/go.dal\n", "-c", "ALL"); err != nil || d != "x" {
		t.Errorf("-c ALL, the only one: %q %v", d, err)
	}
	if _, _, err := resolve(t, "", "-c", "all"); err == nil || err == errNotChosen {
		t.Errorf("-c all without .sdm: %v", err)
	}
	if _, out, err := resolve(t, two, "-c", "3"); err != errNotChosen || !hasTable(out) {
		t.Errorf("-c 3: %v\n%s", err, out)
	}
	if _, _, err := resolve(t, "", "-c", "1"); err == nil || err == errNotChosen {
		t.Errorf("-c without .sdm: %v", err)
	}
	if d, _, err := resolve(t, two, "-dir", `my\cfg`); err != nil || d != "my/cfg" {
		t.Errorf("-dir wins: %q %v", d, err)
	}
	if _, _, err := resolve(t, two, "-dir", "x", "-c", "1"); err == nil || err == errNotChosen {
		t.Errorf("-dir and -c: %v", err)
	}
	if _, out, err := resolve(t, two); err != errNotChosen || !hasTable(out) || strings.Contains(out, "Enter") {
		t.Errorf("several, no console: %v\n%s", err, out)
	}

	console(t, true, "2\r\n")
	if d, out, err := resolve(t, two); err != nil || d != "b/sdm" || !hasTable(out) {
		t.Errorf("console: %q %v\n%s", d, err, out)
	}
	console(t, true, "all\n")
	if d, _, err := resolve(t, two); err != nil || d != "a,b/sdm" {
		t.Errorf("console, all: %q %v", d, err)
	}
	console(t, true, "x\n9\n1\n")
	if d, out, err := resolve(t, two); err != nil || d != "a" || strings.Count(out, "configuration 1..2") != 3 {
		t.Errorf("console, asked again: %q %v\n%s", d, err, out)
	}
	console(t, true, "\n")
	if _, _, err := resolve(t, two); err != errNotChosen {
		t.Errorf("console, Enter: %v", err)
	}
	console(t, true, "")
	if _, _, err := resolve(t, two); err != errNotChosen {
		t.Errorf("console, EOF: %v", err)
	}
}

func TestCommandLine(t *testing.T) {
	got := commandLine("ls", []string{"-l", "-c", "7", "-c=8", "-dir=my cfg", "1,3"})
	if want := `sdmc ls -l "-dir=my cfg" 1,3`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// a command takes its configuration from .sdm.
func TestListByConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	console(t, false, "")
	if err := os.WriteFile("go.mod", []byte("module m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	for _, args := range [][]string{
		{"init", "-dir", "one/sdm", "-dao", "one/dbal", "-orm", "no_orm", "-db", "sqlite"},
		{"init", "-dir", "two/sdm", "-dao", "two/dbal", "-orm", "no_orm", "-db", "sqlite"},
	} {
		if code := run(args, &out, &errOut); code != 0 {
			t.Fatalf("%v: %d %s", args, code, errOut.String())
		}
	}
	if err := os.WriteFile(sdmFile, []byte("one/sdm/go.dal\ntwo/sdm/go.dal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := run([]string{"ls", "-l"}, &out, &errOut); code != 2 ||
		!strings.Contains(squeeze(errOut.String()), "2 two/sdm/go.dal sdmc ls -l -c 2") {
		t.Errorf("ls, several: %d\n%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"ls", "-c", "2", "-l"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "#  kind") {
		t.Errorf("ls -c 2: %d %q %q", code, out.String(), errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"ls", "-c", "3"}, &out, &errOut); code != 2 {
		t.Errorf("ls -c 3: %d %q", code, errOut.String())
	}

	// -c all: each one under its header
	out.Reset()
	if code := run([]string{"ds", "-c", "all"}, &out, &errOut); code != 0 ||
		out.String() != "--- 1:one/sdm/go.dal\nup to date: "+filepath.Join("one", "dbal", "data_store_no_orm.go")+" (no_orm for sqlite)\n\n"+
			"--- 2:two/sdm/go.dal\nup to date: "+filepath.Join("two", "dbal", "data_store_no_orm.go")+" (no_orm for sqlite)\n" {
		t.Errorf("ds -c all: %d %q %q", code, out.String(), errOut.String())
	}
	// a failure does not stop the rest, the exit code is the worst
	if err := os.Remove("one/dbal/data_store_no_orm.go"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"ds", "-c", "all"}, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "sdmc init") || !strings.HasSuffix(out.String(), "--- 2:two/sdm/go.dal\nup to date: "+filepath.Join("two", "dbal", "data_store_no_orm.go")+" (no_orm for sqlite)\n") {
		t.Errorf("ds -c all, one fails: %d %q %q", code, out.String(), errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"xml", "-orm", "sqlx", "-c", "all"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "xml") {
		t.Errorf("xml -c all: %d %q", code, errOut.String())
	}
}

// the folder of a go.dal line has no settings.xml: the error names the line of .sdm.
func TestConfigWithoutSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	console(t, false, "")
	if err := os.WriteFile("go.mod", []byte("module m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"init", "-dir", "huy", "-dao", "huy/dbal", "-orm", "no_orm", "-db", "sqlite"}, &out, &errOut); code != 0 {
		t.Fatalf("init: %d %s", code, errOut.String())
	}
	if err := os.WriteFile(sdmFile, []byte("huy/sdm/go.dal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := run([]string{"ds"}, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "1:huy/sdm/go.dal in .sdm: no settings.xml in huy/sdm; fix the line (<folder of settings.xml>/go.dal) or run sdmc init with -dir huy/sdm") {
		t.Errorf("ds: %d %q", code, errOut.String())
	}
	if err := os.WriteFile(sdmFile, []byte("huy/go.dal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := run([]string{"ds"}, &out, &errOut); code != 0 {
		t.Errorf("ds, fixed line: %d %q", code, errOut.String())
	}
}
