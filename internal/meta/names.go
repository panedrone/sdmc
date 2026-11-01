package meta

import (
	"regexp"
	"strings"
)

// Names of tables in "ref" and "table" of XML may be quoted by the rules of the database, then they may
// have spaces: [Order Details] (SQL Server), "Order Details" (PostgreSQL, SQLite), `Order Details` (MySQL).
// SQL takes such a name as it is written, the catalog of the database wants it without the quotes
// (SqlUtils.split_table_ref, unquote_table_ref of the Java plug-in).

// SplitTableRef returns the parts of "schema.table" split by the dots outside of quotes;
// false if a quote is not closed.
func SplitTableRef(ref string) ([]string, bool) {
	var res []string
	var part strings.Builder
	var closing rune
	r := []rune(ref)
	for i := 0; i < len(r); i++ {
		c := r[i]
		if closing != 0 {
			part.WriteRune(c)
			if c == closing {
				if i+1 < len(r) && r[i+1] == closing {
					part.WriteRune(closing) // ]] or "" inside of a quoted name
					i++
				} else {
					closing = 0
				}
			}
			continue
		}
		if c == '.' {
			res = append(res, part.String())
			part.Reset()
			continue
		}
		switch c {
		case '[':
			closing = ']'
		case '"', '`':
			closing = c
		}
		part.WriteRune(c)
	}
	if closing != 0 {
		return nil, false
	}
	return append(res, part.String()), true
}

// IsQuotedName reports whether s is a name in square brackets, double quotes or backticks.
func IsQuotedName(s string) bool {
	if len(s) < 2 {
		return false
	}
	first, last := s[0], s[len(s)-1]
	return first == '[' && last == ']' || first == '"' && last == '"' || first == '`' && last == '`'
}

// UnquoteName strips the quotes of a quoted name: [Order Details] -> Order Details.
func UnquoteName(s string) string {
	if !IsQuotedName(s) {
		return s
	}
	closing := s[len(s)-1:]
	return strings.ReplaceAll(s[1:len(s)-1], closing+closing, closing)
}

// UnquoteTableRef is the name for the catalog: [dbo].[Order Details] -> dbo.Order Details.
func UnquoteTableRef(ref string) string {
	parts, ok := SplitTableRef(strings.TrimSpace(ref))
	if !ok {
		return ref
	}
	for i, p := range parts {
		parts[i] = UnquoteName(p)
	}
	return strings.Join(parts, ".")
}

var rePlainName = regexp.MustCompile(`^[\p{L}_][\p{L}\p{Nd}_$#@]*$`)

// QuoteName is a table or a schema name for XML: as it is if it is a plain name, otherwise quoted by quote,
// the identifier quote of the database ("[" means [] of SQL Server, "" means no quoting).
func QuoteName(name, quote string) string {
	if rePlainName.MatchString(name) || quote == "" {
		return name
	}
	if quote == "[" {
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	}
	return quote + strings.ReplaceAll(name, quote, quote+quote) + quote
}
