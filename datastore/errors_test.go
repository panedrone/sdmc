package datastore

import (
	"strings"
	"testing"
)

func TestUnknownDatabase(t *testing.T) {
	if _, err := Files(Variants[0], "oracle", "dbal"); err == nil {
		t.Error("Files: no error")
	}
	if _, err := Files("ent", "pg", "dbal"); err == nil {
		t.Error("Files, unknown variant: no error")
	}
	if _, err := Generate(Variants[0], "oracle"); err == nil {
		t.Error("Generate: no error")
	}
	if _, err := PythonText("oracle"); err == nil {
		t.Error("PythonText: no error")
	}
	if _, err := GeneratePython("oracle"); err == nil {
		t.Error("GeneratePython: no error")
	}
	if _, err := CppText("oracle"); err == nil {
		t.Error("CppText: no error")
	}
	if _, err := GenerateCpp("oracle"); err == nil {
		t.Error("GenerateCpp: no error")
	}
}

func TestSpecializeErrors(t *testing.T) {
	for tmpl, want := range map[string]string{
		"// sdm:if oracle\nx\n// sdm:end\n": `unknown database "oracle"`,
		"x\n// sdm:else\n":                  "// sdm:else without // sdm:if",
		"x\n// sdm:end\n":                   "// sdm:end without // sdm:if",
		"// sdm:if pg\nx\n":                 "sdm:if without",
	} {
		_, err := specialize(tmpl, "pg", "// ")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", tmpl, err, want)
		}
	}
	got, err := specialize("a\n// sdm:if pg mysql\nb\n// sdm:else\nc\n// sdm:end\nd\n", "mysql", "// ")
	if err != nil || got != "a\nb\nd\n" {
		t.Errorf("if/else: %q %v", got, err)
	}
}
