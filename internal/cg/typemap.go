package cg

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/panedrone/go-sdm/internal/meta"
	"github.com/panedrone/go-sdm/internal/sdmxml"
)

// SplitTargetType splits "type->assign_func" of type-map. ok == false if there is no function.
func SplitTargetType(target string) (typ, assign string, ok bool) {
	parts := jsplitLit(target, "->")
	if len(parts) > 1 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
	}
	return strings.TrimSpace(target), "", false
}

// TypeMap — <type-map> (JaxbTypeMap.java).
//
// <type detected="...">: two vocabularies on a par: Java names, as in the IDE plug-in
// (java.lang.Integer), and raw driver names (pg: int4, MySQL: INT, SQLite: INTEGER).
// The keys are one map, as in the plug-in. The raw metadata name is used only on the first
// pass (TargetRaw); on a repeated pass of the core the type string itself is looked up, as in Java.
type TypeMap struct {
	detected    map[string]string
	defaultType string
	rawOfJava   func(string) string // meta.Metadata.RawOfJava; nil means unknown
}

// RawOfJava returns a Java name in the words of the database driver; meta.RawUnknown if the database does not know it.
func (m *TypeMap) RawOfJava(javaType string) string {
	if m.rawOfJava != nil {
		if raw := m.rawOfJava(javaType); raw != "" {
			return raw
		}
	}
	return meta.RawUnknown
}

func NewTypeMap(tm sdmxml.TypeMap) *TypeMap {
	res := &TypeMap{detected: map[string]string{}, defaultType: tm.Default}
	for _, t := range tm.Types {
		res.detected[t.Detected] = t.Target
	}
	return res
}

// Unmapped reports a type declared in XML by a Java name of the plug-in (java.lang.Integer) that has
// no key in type-map: the column type from metadata is used instead, and without metadata the same
// Java name in the driver's words (RawOfJava).
func (m *TypeMap) Unmapped(typ string) bool {
	typ = strings.TrimSpace(typ)
	if !strings.HasPrefix(typ, "java.") && typ != "byte[]" {
		return false
	}
	_, ok := m.detected[typ]
	return !ok
}

// Target: detected is an already mapped type or one declared explicitly in XML.
func (m *TypeMap) Target(detected string) string {
	return m.TargetRaw(detected, "")
}

// TargetRaw maps a type from metadata: the Java name detected and the raw driver name raw ("" if none).
// An empty type-map gives the raw name as is: it shows which keys are needed (misc.html#type-map).
func (m *TypeMap) TargetRaw(detected, raw string) string {
	if len(m.detected) == 0 {
		if raw != "" {
			// without the model: "gorm-INT" -> "INT" (the code gets the driver name, not a type-map key)
			// (driver names have no '-')
			if i := strings.IndexByte(raw, '-'); i > 0 {
				return raw[i+1:]
			}
			return raw
		}
		return detected // no overrides: as is, regardless of default
	}
	if t, ok := m.detected[detected]; ok {
		return t
	}
	if t, ok := m.detected[raw]; ok && raw != "" {
		return t
	}
	if strings.TrimSpace(m.defaultType) != "" {
		return m.defaultType
	}
	if raw != "" {
		return raw // no key: as is, the driver name, not the Java one
	}
	return detected
}

// TargetMapped maps a type that is a target already: only by its key, "default" is not for it (a
// non-empty "default" made every mapped field the default; JaxbTypeMap.get_target_type_name_no_default).
func (m *TypeMap) TargetMapped(target string) string {
	if t, ok := m.detected[target]; ok {
		return t
	}
	return target
}

// TargetOf maps the type of a field (JaxbTypeMap.get_target_type_name(FieldInfo)): one from metadata as
// usual, with the raw name of the driver; a mapped one by TargetMapped.
func (m *TypeMap) TargetOf(fi FieldInfo) string {
	if fi.renderedType == fi.originalType {
		return m.TargetRaw(fi.renderedType, fi.rawKey(""))
	}
	return m.TargetMapped(fi.renderedType)
}

// Macros are <macros> (JaxbMacros.java). <vm-xml> is executed (vmxml.go), text
// Velocity (<vm>) is not in the Go version.
type Macros struct {
	custom  map[string]string
	names   []string // custom in order: a HashMap in Java, the result does not depend on the order
	vmXML   map[string][]vmNode
	vmNames []string
	vmText  map[string]bool
	vmErr   map[string]error // a <vm-xml> that failed to parse: the error comes on use
}

func NewMacros(list []sdmxml.Macro) *Macros {
	res := &Macros{custom: map[string]string{}, vmXML: map[string][]vmNode{}, vmText: map[string]bool{},
		vmErr: map[string]error{}}
	for _, m := range list {
		switch {
		case m.Value != nil:
			if *m.Value != "=built-in=" {
				res.custom[m.Name] = *m.Value
			}
		case m.VM != nil:
			res.vmText[m.Name] = true
		case m.VMXML != nil:
			nodes, err := parseVMXML(m.VMXML.Inner)
			if err != nil {
				res.vmErr[m.Name] = err
			}
			res.vmXML[m.Name] = nodes
		}
	}
	for n := range res.custom {
		res.names = append(res.names, n)
	}
	sort.Strings(res.names)
	for n := range res.vmXML {
		res.vmNames = append(res.vmNames, n)
	}
	sort.Strings(res.vmNames)
	return res
}

// ParseTargetType substitutes macros into the target type of a field.
func (m *Macros) ParseTargetType(target string, fi FieldInfo) (string, error) {
	parsed, err := m.parseRecursive(0, target, fi)
	if err != nil {
		return "", err
	}
	return substituteTypeParams(substituteBuiltIn(parsed, fi)), nil
}

func (m *Macros) parseRecursive(depth int, target string, fi FieldInfo) (string, error) {
	if depth > 100 {
		return "", errors.New("depth overflow: " + target)
	}
	found := false
	for _, n := range m.names {
		if strings.Contains(target, n) { // one replacement per pass
			target = strings.ReplaceAll(target, n, m.custom[n])
			found = true
		}
	}
	if !found {
		tmp, ok, err := m.substituteVM(target, fi)
		if err != nil {
			return "", err
		}
		if !ok {
			return target, nil
		}
		target = tmp // one replacement per pass
	}
	return m.parseRecursive(depth+1, target, fi)
}

func (m *Macros) substituteVM(target string, fi FieldInfo) (string, bool, error) {
	for _, n := range m.vmNames {
		if !strings.Contains(target, n) {
			continue
		}
		if err := m.vmErr[n]; err != nil {
			return "", false, fmt.Errorf("%s: %w", n, err)
		}
		var out strings.Builder
		if err := execVM(m.vmXML[n], fi, &out); err != nil {
			return "", false, fmt.Errorf("%s: %w", n, err)
		}
		return strings.ReplaceAll(target, n, out.String()), true, nil
	}
	for n := range m.vmText {
		if strings.Contains(target, n) {
			return "", false, errors.New("Velocity macros (<vm>) are not supported by go-sdm, use <vm-xml>: " + n)
		}
	}
	return target, false, nil
}

var builtIn = []struct {
	name string
	fn   func(FieldInfo) string
}{
	{"${lower_snake_case(column)}", func(f FieldInfo) string { return CamelCaseToLowerSnakeCase(f.columnName) }},
	{"${camelCase(column)}", func(f FieldInfo) string { return LowerCamel(f.columnName) }},
	{"${TitleCase(column)}", func(f FieldInfo) string { return Title(f.columnName) }},
	{"${kebab-case(column)}", func(f FieldInfo) string { return ToKebabCase(f.columnName) }},
	{"${column}", func(f FieldInfo) string { return f.columnName }},
}

func substituteBuiltIn(t string, fi FieldInfo) string {
	if t == "" {
		return t
	}
	for _, b := range builtIn {
		if strings.Contains(t, b.name) {
			t = strings.ReplaceAll(t, b.name, b.fn(fi))
		}
	}
	return t
}

// substituteTypeParams: "type${0}|0:value" -> "typevalue"; unsubstituted ${...} are removed.
func substituteTypeParams(t string) string {
	if start := strings.IndexByte(t, '|'); start != -1 {
		params := map[string]string{}
		for _, p := range jsplit(t[start:], reBar) {
			i := strings.IndexByte(p, ':')
			if i == -1 {
				continue
			}
			params["${"+p[:i]+"}"] = p[i+1:]
		}
		t = t[:start]
		names := make([]string, 0, len(params))
		for n := range params {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			t = strings.ReplaceAll(t, n, params[n])
		}
	}
	for _, call := range findMacroCalls(t) {
		t = strings.ReplaceAll(t, call, "")
	}
	return t
}

func findMacroCalls(s string) []string {
	var res []string
	inside, start := false, -1
	for i := 0; i < len(s); i++ {
		if strings.HasPrefix(s[i:], "${") {
			inside, start = true, i
			continue
		}
		if inside && s[i] == '}' {
			res = append(res, s[start:i+1])
			inside, start = false, -1
		}
	}
	return res
}

// newTypeMapFor returns the type-map and the vocabulary of Java names of database md.
func newTypeMapFor(md meta.Metadata, tm sdmxml.TypeMap) *TypeMap {
	res := NewTypeMap(tm)
	res.rawOfJava = md.RawOfJava
	return res
}
