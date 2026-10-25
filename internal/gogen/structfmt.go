package gogen

import (
	"fmt"
	"strings"

	"github.com/panedrone/sdmc/internal/cg"
	"github.com/panedrone/sdmc/internal/sdmxml"
)

// Alignment of DTO struct fields (GoStructFormatter.java): names, types and tags
// in columns, as gofmt would do it, plus the fields of <custom>.

type customField struct {
	name, typ, tag string
	comment        *string
}

type block struct {
	maxLen int
}

func formatFields(dto *sdmxml.DtoClass, fields []cg.FieldInfo) ([]string, []cg.FieldInfo, error) {
	custom := parseCustom(dto.Custom)
	for _, cf := range custom {
		if cf.typ == "" {
			continue
		}
		fi, err := cg.NewFieldInfo(cg.AsIs, "", "", "")
		if err != nil {
			return nil, nil, err
		}
		fields = append(fields, fi.WithName(cf.name).WithRenderedType(fmt.Sprintf("%s %s", cf.typ, cf.tag)))
	}
	var res []string
	maxName := -1
	for _, fi := range fields {
		if fi.Name() == "" { // {base}: the embedded type goes first
			res = append(res, typeWithoutImportAndTag(fi.Type()))
			continue
		}
		if n := len([]rune(fi.Name())); n > maxName {
			maxName = n
		}
	}
	byName := map[string]customField{}
	for _, cf := range custom {
		if cf.name != "" {
			byName[cf.name] = cf
		}
	}
	tpBlocks := typeBlocks(byName, fields)
	tagBlocks := tagBlocks(byName, fields)
	for _, fi := range fields {
		if fi.Name() == "" {
			continue
		}
		justType := typeWithoutImportAndTag(fi.Type())
		typeName := typeWithoutImport(fi.Type())
		justTpLen := len([]rune(justType))
		tp := justType
		if b := tpBlocks[fi.Name()]; b != nil && b.maxLen > 0 {
			tp = padRight(justType, b.maxLen)
		}
		if justTpLen < len([]rune(typeName)) { // has a tag
			tag := strings.TrimSpace(string([]rune(typeName)[justTpLen+1:]))
			if b := tagBlocks[fi.Name()]; b != nil && b.maxLen > 0 {
				tag = padRight(tag, b.maxLen)
			}
			typeName = tp + " " + tag // no trim
		} else {
			typeName = tp // no trim
		}
		name := padRight(fi.Name(), maxName)
		var line string
		cf, hasCustom := byName[fi.Name()]
		switch {
		case !hasCustom || cf.comment == nil:
			line = strings.TrimSpace(name + " " + typeName)
		case *cf.comment == "":
			line = name + " " + typeName + " //"
		default:
			line = name + " " + typeName + " // " + strings.TrimSpace(*cf.comment)
		}
		res = append(res, line)
	}
	return res, fields, nil
}

func hasComment(byName map[string]customField, name string) bool {
	cf, ok := byName[name]
	return ok && cf.comment != nil
}

// typeBlocks: consecutive fields with a tag or a comment align types by the longest one.
func typeBlocks(byName map[string]customField, fields []cg.FieldInfo) map[string]*block {
	res := map[string]*block{}
	var cur *block
	for _, fi := range fields {
		if fi.Name() == "" {
			continue
		}
		justLen := len([]rune(typeWithoutImportAndTag(fi.Type())))
		tagExists := justLen < len([]rune(typeWithoutImport(fi.Type())))
		if !tagExists && !hasComment(byName, fi.Name()) {
			cur = nil
		} else {
			if cur == nil {
				cur = &block{}
			}
			if justLen > cur.maxLen {
				cur.maxLen = justLen
			}
		}
		res[fi.Name()] = cur
	}
	return res
}

// tagBlocks: consecutive fields with a comment align tags.
func tagBlocks(byName map[string]customField, fields []cg.FieldInfo) map[string]*block {
	res := map[string]*block{}
	var cur *block
	for _, fi := range fields {
		if fi.Name() == "" {
			continue
		}
		if !hasComment(byName, fi.Name()) {
			cur = nil
		} else {
			justType := typeWithoutImportAndTag(fi.Type())
			typeName := typeWithoutImport(fi.Type())
			justLen := len([]rune(justType))
			if justLen < len([]rune(typeName)) {
				if cur == nil {
					cur = &block{}
				}
				tag := strings.TrimSpace(string([]rune(typeName)[justLen+1:]))
				if n := len([]rune(tag)); n > cur.maxLen {
					cur.maxLen = n
				}
			}
		}
		res[fi.Name()] = cur
	}
	return res
}

const base = "{base}"

func parseCustom(custom string) []customField {
	var res []customField
	for _, line := range jsplit(custom, reCRLF) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var cf customField
		if strings.HasPrefix(line, base) {
			tp := strings.TrimSpace(line[len(base):])
			if tp == "" {
				continue
			}
			cf.typ = tp // {base} has no comments
			res = append(res, cf)
			continue
		}
		if pos := strings.Index(line, "//"); pos >= 0 {
			parseCustomField(strings.TrimSpace(line[:pos]), &cf)
			c := line[pos+2:]
			cf.comment = &c
		} else {
			parseCustomField(line, &cf)
		}
		res = append(res, cf)
	}
	return res
}

func parseCustomField(line string, cf *customField) {
	parts := jsplit(line, reWS)
	if len(parts) > 0 {
		cf.name = parts[0]
	}
	if len(parts) > 1 {
		cf.typ = parts[1] // no spaces inside the type
	}
	if len(parts) > 2 {
		if pos := strings.Index(line, cf.name); pos != -1 {
			after := line[pos+len(cf.name):]
			if pt := strings.Index(after, cf.typ); pt != -1 {
				cf.tag = strings.TrimSpace(after[pt+len(cf.typ):])
			}
		}
	}
}
