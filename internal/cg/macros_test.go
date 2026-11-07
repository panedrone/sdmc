package cg

import (
	"strings"
	"testing"

	"github.com/panedrone/sdmc/internal/sdmxml"
)

func str(s string) *string { return &s }

func vmMacro(name, inner string) sdmxml.Macro {
	m := sdmxml.Macro{Name: name}
	m.VMXML = &struct {
		Inner string `xml:",innerxml"`
	}{Inner: inner}
	return m
}

func TestMacros(t *testing.T) {
	ms := NewMacros([]sdmxml.Macro{
		{Name: "${column}", Value: str("=built-in=")},
		{Name: "${json}", Value: str(" `json:\"${lower_snake_case(column)}${0}\"`")},
		{Name: "${loop}", Value: str("${loop}x")},
		{Name: "${old}", VM: str("#if($fi.PK)pk#end")},
		vmMacro("${size}", `<if var="$fi.ColumnSize > 0"><print var=";size:$fi.ColumnSize"/></if>`),
		vmMacro("${broken}", `<foreach/>`),
		vmMacro("${bad-expr}", `<if var="(("><print var="x"/></if>`),
	})
	f, _ := NewFieldInfo(AsIs, "string", "TaskSubject", "")
	f = f.WithColumnSize(256)
	for target, want := range map[string]string{
		"string${json}|0:,omitempty":                "string `json:\"task_subject,omitempty\"`",
		"string${json}":                             "string `json:\"task_subject\"`",
		"string `gorm:\"column:${column}${size}\"`": "string `gorm:\"column:TaskSubject;size:256\"`",
		"${camelCase(column)}":                      "taskSubject",
		"${TitleCase(column)}":                      "TaskSubject",
		"${kebab-case(column)}":                     "task-subject",
		"int64|bad|1:x":                             "int64",
		"":                                          "",
	} {
		got, err := ms.ParseTargetType(target, f)
		if err != nil {
			t.Errorf("%q: %v", target, err)
			continue
		}
		if got != want {
			t.Errorf("%q: %q, want %q", target, got, want)
		}
	}
	for target, want := range map[string]string{
		"${loop}":      "depth overflow",
		"x${old}":      "Velocity macros (<vm>) are not supported",
		"x${broken}":   "${broken}",
		"x${bad-expr}": "${bad-expr}",
	} {
		_, err := ms.ParseTargetType(target, f)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", target, err, want)
		}
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string]string{"TaskSubject": "task-subject", "t_id": "t-id", "": ""} {
		if got := ToKebabCase(in); got != want {
			t.Errorf("ToKebabCase(%q) = %q, want %q", in, got, want)
		}
	}
}
