package cg

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// <vm-xml> is a type-map macro in XML form (Xml2Vm.java translates it to Velocity, here it
// is executed directly). Supported is what occurs in presets and projects: <if>,
// <elseif>, <else>, <print>, <br/>. Expressions are as in Velocity: $fi.Prop, literals,
// ! && || == != > < >= <=, parentheses. Unknown $-references in <print> stay as they are,
// as Velocity leaves them (${column}, ${0} will be substituted by type-map).

type vmNode struct {
	kind     string // if, elseif, else, print, br
	expr     string // if/elseif: var; print: var
	children []vmNode
	branches []vmNode // if: elseif/else in order
}

func parseVMXML(inner string) ([]vmNode, error) {
	d := xml.NewDecoder(strings.NewReader("<vm-xml>" + inner + "</vm-xml>"))
	if _, err := d.Token(); err != nil { // <vm-xml>
		return nil, err
	}
	return parseVMChildren(d, "vm-xml")
}

func parseVMChildren(d *xml.Decoder, parent string) ([]vmNode, error) {
	var res []vmNode
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, errors.New("unexpected end of <vm-xml>")
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return res, nil
		case xml.StartElement:
			n := vmNode{kind: t.Name.Local}
			for _, a := range t.Attr {
				if a.Name.Local == "var" {
					n.expr = a.Value
				}
			}
			switch n.kind {
			case "if", "elseif", "else", "print", "br":
			default:
				return nil, fmt.Errorf("<vm-xml>: <%s> is not supported by sdmc", n.kind)
			}
			children, err := parseVMChildren(d, n.kind)
			if err != nil {
				return nil, err
			}
			if n.kind == "if" { // <elseif>/<else> are children of <if>, but go to branches
				for _, c := range children {
					if c.kind == "elseif" || c.kind == "else" {
						n.branches = append(n.branches, c)
					} else {
						n.children = append(n.children, c)
					}
				}
			} else {
				n.children = children
			}
			res = append(res, n)
		}
	}
}

func execVM(nodes []vmNode, fi FieldInfo, out *strings.Builder) error {
	for _, n := range nodes {
		switch n.kind {
		case "print":
			out.WriteString(interpolate(n.expr, fi))
		case "br":
			out.WriteString("\n")
		case "if":
			ok, err := evalBool(n.expr, fi)
			if err != nil {
				return err
			}
			if ok {
				if err := execVM(n.children, fi, out); err != nil {
					return err
				}
				continue
			}
			for _, b := range n.branches {
				if b.kind == "else" {
					if err := execVM(b.children, fi, out); err != nil {
						return err
					}
					break
				}
				ok, err := evalBool(b.expr, fi)
				if err != nil {
					return err
				}
				if ok {
					if err := execVM(b.children, fi, out); err != nil {
						return err
					}
					break
				}
			}
		default: // elseif/else outside <if>
			return fmt.Errorf("<vm-xml>: unexpected <%s>", n.kind)
		}
	}
	return nil
}

// FieldInfo properties as Velocity sees them: $fi.PK -> isPK(), $fi.ColumnSize -> getColumnSize().
var vmProps = map[string]func(FieldInfo) any{
	"PK":                 func(f FieldInfo) any { return f.pk },
	"AI":                 func(f FieldInfo) any { return f.ai },
	"Nullable":           func(f FieldInfo) any { return f.nullable },
	"Indexed":            func(f FieldInfo) any { return f.indexed },
	"Unique":             func(f FieldInfo) any { return f.unique },
	"ColumnSize":         func(f FieldInfo) any { return f.ColumnSize() },
	"Precision":          func(f FieldInfo) any { return f.precision },
	"Scale":              func(f FieldInfo) any { return f.scale },
	"DisplaySize":        func(f FieldInfo) any { return f.displaySize },
	"DecimalDigits":      func(f FieldInfo) any { return f.decimalDigits },
	"Name":               func(f FieldInfo) any { return f.name },
	"LowerCamelCaseName": func(f FieldInfo) any { return f.lowerCamel },
	"Type":               func(f FieldInfo) any { return f.renderedType },
	"ColumnName":         func(f FieldInfo) any { return f.columnName },
	"Comment":            func(f FieldInfo) any { return f.comment },
	"AssignFunc":         func(f FieldInfo) any { return f.assignFunc },
	"OriginalType":       func(f FieldInfo) any { return f.originalType },
	"OriginalScalarType": func(f FieldInfo) any { return f.originalScalarType },
	"ScalarType":         func(f FieldInfo) any { return f.scalarType },
	"FK": func(f FieldInfo) any {
		if f.fk == "" {
			return nil
		}
		return f.fk
	},
	"Model": func(f FieldInfo) any {
		if f.model == "" {
			return nil
		}
		return f.model
	},
}

func vmProp(name string, fi FieldInfo) any {
	if p, ok := vmProps[name]; ok {
		return p(fi)
	}
	return nil
}

var reVMRef = regexp.MustCompile(`\$!?\{?fi\.([A-Za-z_][A-Za-z0-9_]*)\}?`)

func interpolate(text string, fi FieldInfo) string {
	return reVMRef.ReplaceAllStringFunc(text, func(ref string) string {
		m := reVMRef.FindStringSubmatch(ref)
		braced := strings.Contains(ref, "{")
		if braced != strings.HasSuffix(ref, "}") {
			return ref // "${fi.X" without "}" is not a reference
		}
		v := vmProp(m[1], fi)
		if v == nil {
			if strings.HasPrefix(ref, "$!") {
				return ""
			}
			return ref // Velocity prints an undefined reference as is
		}
		return fmt.Sprint(v)
	})
}

// Expressions of <if var="..."> ------------------------------------------

type vmParser struct {
	toks []string
	pos  int
	fi   FieldInfo
}

func evalBool(expr string, fi FieldInfo) (bool, error) {
	toks, err := tokenizeVM(expr)
	if err != nil {
		return false, err
	}
	p := &vmParser{toks: toks, fi: fi}
	v, err := p.or()
	if err != nil {
		return false, err
	}
	if p.pos != len(p.toks) {
		return false, fmt.Errorf("<vm-xml>: unexpected '%s' in '%s'", p.toks[p.pos], expr)
	}
	return truthy(v), nil
}

func tokenizeVM(s string) ([]string, error) {
	var res []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case strings.HasPrefix(s[i:], "&&") || strings.HasPrefix(s[i:], "||") ||
			strings.HasPrefix(s[i:], "==") || strings.HasPrefix(s[i:], "!=") ||
			strings.HasPrefix(s[i:], ">=") || strings.HasPrefix(s[i:], "<="):
			res = append(res, s[i:i+2])
			i += 2
		case strings.ContainsRune("!<>()", rune(c)):
			res = append(res, string(c))
			i++
		case c == '"' || c == '\'':
			j := strings.IndexByte(s[i+1:], c)
			if j < 0 {
				return nil, errors.New("<vm-xml>: unterminated string in '" + s + "'")
			}
			res = append(res, s[i:i+j+2])
			i += j + 2
		default:
			j := i
			for j < len(s) && (s[j] == '$' || s[j] == '.' || s[j] == '{' || s[j] == '}' || s[j] == '!' && j == i+1 ||
				unicode.IsLetter(rune(s[j])) || unicode.IsDigit(rune(s[j])) || s[j] == '_' || s[j] == '-' && j == i) {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("<vm-xml>: unexpected '%c' in '%s'", c, s)
			}
			res = append(res, s[i:j])
			i = j
		}
	}
	return res, nil
}

func (p *vmParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *vmParser) next() string {
	t := p.peek()
	p.pos++
	return t
}

func (p *vmParser) or() (any, error) {
	l, err := p.and()
	for err == nil && (p.peek() == "||" || p.peek() == "or") {
		p.next()
		var r any
		if r, err = p.and(); err == nil {
			l = truthy(l) || truthy(r)
		}
	}
	return l, err
}

func (p *vmParser) and() (any, error) {
	l, err := p.cmp()
	for err == nil && (p.peek() == "&&" || p.peek() == "and") {
		p.next()
		var r any
		if r, err = p.cmp(); err == nil {
			l = truthy(l) && truthy(r)
		}
	}
	return l, err
}

func (p *vmParser) cmp() (any, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	switch op := p.peek(); op {
	case "==", "!=", ">", "<", ">=", "<=", "eq", "ne", "gt", "lt", "ge", "le":
		p.next()
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		return compare(op, l, r), nil
	}
	return l, nil
}

func (p *vmParser) unary() (any, error) {
	switch t := p.peek(); {
	case t == "!" || t == "not":
		p.next()
		v, err := p.unary()
		return !truthy(v), err
	case t == "(":
		p.next()
		v, err := p.or()
		if err == nil && p.next() != ")" {
			err = errors.New("<vm-xml>: ')' expected")
		}
		return v, err
	case t == "":
		return nil, errors.New("<vm-xml>: unexpected end of expression")
	}
	t := p.next()
	switch {
	case t == "true":
		return true, nil
	case t == "false":
		return false, nil
	case t[0] == '"' || t[0] == '\'':
		return t[1 : len(t)-1], nil
	case strings.HasPrefix(t, "$"):
		m := reVMRef.FindStringSubmatch(t)
		if m == nil || m[0] != t {
			return nil, errors.New("<vm-xml>: only $fi.Property references are supported: " + t)
		}
		return vmProp(m[1], p.fi), nil
	}
	if n, err := strconv.Atoi(t); err == nil {
		return n, nil
	}
	return nil, errors.New("<vm-xml>: unexpected '" + t + "'")
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	}
	return true // Velocity 1.7: non-null is true
}

func compare(op string, l, r any) bool {
	li, lok := l.(int)
	ri, rok := r.(int)
	if lok && rok {
		switch op {
		case "==", "eq":
			return li == ri
		case "!=", "ne":
			return li != ri
		case ">", "gt":
			return li > ri
		case "<", "lt":
			return li < ri
		case ">=", "ge":
			return li >= ri
		case "<=", "le":
			return li <= ri
		}
	}
	ls, rs := fmt.Sprint(l), fmt.Sprint(r)
	switch op {
	case "==", "eq":
		return l != nil && r != nil && ls == rs || l == nil && r == nil
	case "!=", "ne":
		return !(l != nil && r != nil && ls == rs || l == nil && r == nil)
	}
	return false // Velocity does not compare non-numbers with > <
}
