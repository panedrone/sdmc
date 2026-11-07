package sdmxml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sdmDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadSdmErrors(t *testing.T) {
	for _, c := range []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"sdm.xml": `<sdm><dto-class name="A" ref="a"/><dto-class name="A" ref="b"/></sdm>`}, `duplicate name="A"`},
		{map[string]string{"sdm.xml": `<sdm><dto-class name="A" ref="a"/><dao-class name="A"/></sdm>`}, `duplicate name="A"`},
		{map[string]string{"sdm.xml": `<sdm><dto-class ref="a"/></sdm>`}, "empty 'name'"},
		{map[string]string{"sdm.xml": `<sdm><dao-class name="D" ref="d.xml"><crud dto="A"/></dao-class></sdm>`}, "both ref"},
		{map[string]string{"sdm.xml": `<sdm><dao-class name="D" ref="d.xml"/></sdm>`, "d.xml": `<sdm/>`}, "<dao-class> expected"},
		{map[string]string{"sdm.xml": `<sdm><dao-class name="D" ref="d.xml"/></sdm>`, "d.xml": `<dao-class><querry/></dao-class>`}, "d.xml"},
		{map[string]string{"sdm.xml": `<sdm><dao-class name="D" ref="missing.xml"/></sdm>`}, "missing.xml"},
		{map[string]string{"sdm.xml": `<sdm><dto-class name="A"`}, ""},
	} {
		_, err := LoadSdm(sdmDir(t, c.files))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.files, err, c.want)
		}
	}
	// unknown top-level elements are skipped, as in the plug-in
	s, err := LoadSdm(sdmDir(t, map[string]string{"sdm.xml": `<sdm><comment-block>x</comment-block><dto-class name="A" ref="a"/></sdm>`}))
	if err != nil || len(s.DtoClasses) != 1 {
		t.Errorf("unknown element: %v", err)
	}
}

func TestLoadSettingsErrors(t *testing.T) {
	const head = `<settings><folders target="m" sql="sql"/><dto scope="d"/><dao scope="d"/>`
	for xml, want := range map[string]string{
		head + `<type-map default=""><type detected="int" target="a"/><type detected="int" target="b"/></type-map></settings>`: `duplicate <type detected="int"`,
		head + `<macros><macro name="${a}" value="1"/><macro name="${a}" value="2"/></macros></settings>`:                      `duplicate <macro name="${a}"`,
	} {
		_, err := LoadSettings(sdmDir(t, map[string]string{"settings.xml": xml}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", xml, err, want)
		}
	}
}
