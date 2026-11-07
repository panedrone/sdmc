package cg

import (
	"testing"

	"github.com/panedrone/sdmc/internal/sdmxml"
)

func TestTypeMapRaw(t *testing.T) {
	tm := NewTypeMap(sdmxml.TypeMap{Types: []sdmxml.Type{
		{Detected: "int4", Target: "int32${json} -> SetInt32"},
		{Detected: "gorm-int4", Target: "int32${json-gorm} -> SetInt32"},
		{Detected: "gorm-uuid+auto", Target: "uuid.UUID|1:;default:gen_random_uuid()"},
		{Detected: "java.lang.Long", Target: "int64-from-java"}, // a Java key takes precedence over a raw one
		{Detected: "int8", Target: "int64-from-raw"},
		{Detected: "text", Target: "string${json} -> SetString"},
	}})
	cases := []struct{ detected, raw, want string }{
		{"java.lang.Integer", "int4", "int32${json} -> SetInt32"},
		{"gorm-java.lang.Integer", "gorm-int4", "int32${json-gorm} -> SetInt32"},
		{"gorm-java.util.UUID+auto", "gorm-uuid+auto", "uuid.UUID|1:;default:gen_random_uuid()"},
		{"java.lang.Long", "int8", "int64-from-java"},
		{"java.lang.String", "varchar", "varchar"}, // neither one nor the other: the driver name
		// second pass (raw == ""): the keys are one map, as in the plug-in: a string matching
		// a key is remapped (for compatibility with plug-in configs: "Object", "int", ...)
		{"text", "", "string${json} -> SetString"},
	}
	for _, c := range cases {
		if got := tm.TargetRaw(c.detected, c.raw); got != c.want {
			t.Errorf("%s / %s: got %q, want %q", c.detected, c.raw, got, c.want)
		}
	}
}

func TestTypeMapEmpty(t *testing.T) {
	tm := NewTypeMap(sdmxml.TypeMap{})
	if got := tm.TargetRaw("java.lang.Integer", "INTEGER"); got != "INTEGER" {
		t.Errorf("empty type-map, raw known: %q", got)
	}
	if got := tm.TargetRaw("java.lang.Object", ""); got != "java.lang.Object" {
		t.Errorf("empty type-map, raw unknown: %q", got)
	}
}

// TestTypeMapDriverOnly: C++ looks up the driver name of a column only; a Java key does not map it,
// and without a driver key the code gets the driver name for the compiler to report.
func TestTypeMapDriverOnly(t *testing.T) {
	tm := NewTypeMap(sdmxml.TypeMap{Types: []sdmxml.Type{
		{Detected: "java.lang.Long", Target: "CJava"},
		{Detected: "int8", Target: "CInteger"},
		{Detected: "java.lang.String", Target: "CText"},
		{Detected: "CText", Target: "CTextX"},
	}})
	tm.DriverNamesOnly()
	cases := []struct{ detected, raw, want string }{
		{"java.lang.Long", "int8", "CInteger"},
		{"java.lang.String", "varchar", "varchar"},       // a Java key only: the driver name
		{"java.lang.String", "model-varchar", "varchar"}, // without the model
		{"java.lang.Long", "", "CJava"},                  // written in XML, no metadata: as before
		{"CText", "", "CTextX"},                          // second pass: as before
	}
	for _, c := range cases {
		if got := tm.TargetRaw(c.detected, c.raw); got != c.want {
			t.Errorf("%s / %s: got %q, want %q", c.detected, c.raw, got, c.want)
		}
	}
	tm = NewTypeMap(sdmxml.TypeMap{Default: "CVariant", Types: []sdmxml.Type{{Detected: "java.lang.String", Target: "CText"}}})
	tm.DriverNamesOnly()
	if got := tm.TargetRaw("java.lang.String", "varchar"); got != "CVariant" {
		t.Errorf("default: got %q", got)
	}
}
