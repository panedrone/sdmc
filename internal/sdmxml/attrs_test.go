package sdmxml

import (
	"os"
	"path/filepath"
	"testing"
)

// XML 1.0, 3.3.3: the line breaks and tabs of attribute values are spaces, as for JAXB of the plug-in.
func TestNormalizeAttrs(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"<q ref=\"\nselect 1;\nselect 2;\"/>", `<q ref=" select 1; select 2;"/>`},
		{"<q ref=\"a\r\nb\rc\td\"/>", `<q ref="a b c d"/>`},
		{"<q ref='a\nb' m=\"x\ny\"/>", `<q ref='a b' m="x y"/>`},
		{"<q ref=\"a&#10;b\"/>", `<q ref="a&#10;b"/>`},
		{"<q ref=\"it's\n\"/>", `<q ref="it's "/>`},
		{"<q>\ntext\n</q>", "<q>\ntext\n</q>"},
		{"<!-- a=\"\n\" -->\n<q/>", "<!-- a=\"\n\" -->\n<q/>"},
		{"<q><![CDATA[a=\"\n\"]]></q>", "<q><![CDATA[a=\"\n\"]]></q>"},
		{"<?xml version=\"1.0\"?>\n<q\n  a=\"1\"\n  b=\"2\n3\"/>", "<?xml version=\"1.0\"?>\n<q\n  a=\"1\"\n  b=\"2 3\"/>"},
	} {
		if got := string(normalizeAttrs([]byte(c.in))); got != c.want {
			t.Errorf("%q:\n got  %q\n want %q", c.in, got, c.want)
		}
	}
}

func TestLoadSdmMultiLineRef(t *testing.T) {
	dir := t.TempDir()
	sdm := "<sdm>\r\n    <dao-class name=\"SpDao\">\r\n        <query method=\"rv\" ref=\"\r\nDECLARE @rv int;\r\n" +
		"EXEC @rv = test_sp_return_value;\r\nSELECT @rv;\"/>\r\n    </dao-class>\r\n</sdm>\r\n"
	if err := os.WriteFile(filepath.Join(dir, "sdm.xml"), []byte(sdm), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSdm(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := " DECLARE @rv int; EXEC @rv = test_sp_return_value; SELECT @rv;"
	if got := s.DaoClasses[0].Items[0].Ref; got != want {
		t.Errorf("ref:\n got  %q\n want %q", got, want)
	}
}
