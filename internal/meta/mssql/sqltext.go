package mssql

import (
	"strconv"
	"strings"
)

// toNative turns JDBC SQL into T-SQL: {call p(?, ?)} -> EXEC p @P1, @P2, and each '?' outside of
// 'strings', [names], "names" and comments -> @P1..@Pn, as mssql-jdbc sends it. proc is the name of
// the procedure of a call ("" if it is not a call).
func toNative(jdbcSQL string) (native string, n int, proc string) {
	sql := strings.TrimSpace(jdbcSQL)
	if strings.HasPrefix(sql, "{") && strings.HasSuffix(sql, "}") {
		sql = strings.TrimSpace(sql[1 : len(sql)-1])
	}
	if word, rest := firstWord(sql); strings.EqualFold(word, "call") || strings.EqualFold(word, "exec") ||
		strings.EqualFold(word, "execute") {
		rest = strings.TrimSpace(rest)
		end := nameEnd(rest)
		name, args := rest[:end], strings.TrimSpace(rest[end:])
		if strings.HasPrefix(args, "(") && strings.HasSuffix(args, ")") {
			args = strings.TrimSpace(args[1 : len(args)-1]) // call p(...)
		}
		proc = name
		sql = "EXEC " + name
		if args != "" {
			sql += " " + args
		}
	}
	native, n = placeholders(sql)
	return native, n, proc
}

func firstWord(sql string) (word, rest string) {
	i := strings.IndexAny(sql, " \t\r\n(")
	if i < 0 {
		return sql, ""
	}
	return sql[:i], sql[i:]
}

// nameEnd is the end of a (schema.)name at the start of s: [names] and "names" may have spaces.
func nameEnd(s string) int {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '[':
			i = skipQuoted(s, i, ']') - 1
		case c == '"':
			i = skipQuoted(s, i, '"') - 1
		case c == '(' || c == ' ' || c == '\t' || c == '\r' || c == '\n':
			return i
		}
	}
	return len(s)
}

func placeholders(sql string) (string, int) {
	var b strings.Builder
	n := 0
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '\'':
			j := skipQuoted(sql, i, '\'')
			b.WriteString(sql[i:j])
			i = j - 1
		case c == '"':
			j := skipQuoted(sql, i, '"')
			b.WriteString(sql[i:j])
			i = j - 1
		case c == '[':
			j := skipQuoted(sql, i, ']')
			b.WriteString(sql[i:j])
			i = j - 1
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			j := strings.IndexByte(sql[i:], '\n')
			if j < 0 {
				j = len(sql) - i
			}
			b.WriteString(sql[i : i+j])
			i += j - 1
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			j := strings.Index(sql[i+2:], "*/")
			end := len(sql)
			if j >= 0 {
				end = i + 2 + j + 2
			}
			b.WriteString(sql[i:end])
			i = end - 1
		case c == '?':
			n++
			b.WriteString("@P" + strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), n
}

// skipQuoted returns the position after the closing quote; a doubled closing quote is a quote inside.
func skipQuoted(sql string, start int, closing byte) int {
	for i := start + 1; i < len(sql); i++ {
		if sql[i] == closing {
			if i+1 < len(sql) && sql[i+1] == closing {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(sql)
}
