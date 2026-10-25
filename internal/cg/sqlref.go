package cg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The grammar of 'ref' (SqlUtils.java). The branches and the order of checks are as in Java,
// oddities included: what the plug-in generates depends on them.

type sqlShortcut struct {
	tableName string
	params    string // "" means none
	hasParams bool
	colNames  string
	hasCols   bool
}

func IsEmptyRef(ref string) bool {
	return strings.TrimSpace(ref) == ""
}

func isSQLFileRefBase(ref string) bool {
	return len(strings.TrimSpace(ref)) > 4 && strings.HasSuffix(ref, ".sql")
}

func IsSQLFileRef(ref string) bool {
	if IsSQLShortcutRef(ref) || IsJdbcStoredProcCall(ref) {
		return false
	}
	return isSQLFileRefBase(ref)
}

func IsSQLShortcutRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || isSQLFileRefBase(ref) {
		return false
	}
	shc, err := parseSQLShortcutRef(ref)
	if err != nil {
		return false
	}
	if shc.hasParams {
		_, err := ListedItems(shc.params, false)
		return err == nil
	}
	return shc.hasCols
}

var illegalTableChars = "/\n\r\t\x00\f?*\\<>|\";,(){}"

func IsTableRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || reWS.MatchString(ref) {
		return false
	}
	if IsSQLShortcutRef(ref) || IsJdbcStoredProcCall(ref) || IsSQLFileRef(ref) ||
		isSpCallShortcut(ref) || isUdfCallShortcut(ref) {
		return false
	}
	if strings.ContainsAny(ref, illegalTableChars) {
		return false
	}
	return !slices.Contains(strings.Split(ref, "."), "") // no empty parts between dots
}

func IsJdbcStoredProcCall(sql string) bool {
	sql = strings.TrimSpace(sql)
	open, closed := strings.HasPrefix(sql, "{"), strings.HasSuffix(sql, "}")
	if open && closed {
		sql = sql[1 : len(sql)-1]
	} else if open != closed {
		return false
	}
	return isSpCallShortcut(sql)
}

func firstWordIs(text, word string) bool {
	parts := jsplit(text, reWS)
	return len(parts) >= 2 && strings.EqualFold(strings.TrimSpace(parts[0]), word)
}

func isSpCallShortcut(text string) bool  { return firstWordIs(text, "call") }
func isUdfCallShortcut(text string) bool { return firstWordIs(text, "select") }

func parseSQLShortcutRef(ref string) (sqlShortcut, error) {
	var res sqlShortcut
	ref = strings.TrimSpace(ref)
	mm := jsplitLit(ref, "->")
	if len(mm) != 2 {
		mm = jsplitLit(ref, "/")
	}
	if len(mm) == 2 {
		ref = strings.TrimSpace(mm[0])
		res.colNames, res.hasCols = strings.TrimSpace(mm[1]), true
	} else if len(mm) > 2 {
		return res, errors.New("Invalid 'ref' in SQL-shortcut: " + ref)
	}
	ref = strings.TrimSpace(ref)
	pos := strings.IndexByte(ref, '(')
	if strings.HasSuffix(ref, ")") {
		if pos < 1 {
			return res, errors.New("Invalid 'ref' in SQL-shortcut: '(' expected")
		}
		res.tableName = strings.TrimSpace(ref[:pos])
		res.params = strings.TrimSpace(ref[pos+1 : len(ref)-1])
		res.hasParams = res.params != ""
	} else {
		res.tableName = ref
	}
	return res, nil
}

func sqlShortcutToJdbcSQL(ref string) (string, error) {
	shc, err := parseSQLShortcutRef(ref)
	if err != nil {
		return "", err
	}
	params := ""
	if shc.hasParams && strings.TrimSpace(shc.params) != "" {
		arr, err := ListedItems(shc.params, false)
		if err != nil {
			return "", err
		}
		if len(arr) < 1 {
			return "", errors.New("Not empty list of parameters expected in SQL shortcut")
		}
		params = arr[0] + "=?"
		for _, p := range arr[1:] {
			params += " and " + p + "=?"
		}
	}
	var sql string
	if !shc.hasCols {
		sql = jdbcSQLByTableName(shc.tableName)
	} else {
		sql = "select " + shc.colNames + " from " + shc.tableName
	}
	if params == "" {
		return sql, nil
	}
	return sql + " where " + params, nil
}

func jdbcSQLByTableName(table string) string {
	return "select * from " + table
}

// loadTextFromFile checks the case of the file name on Windows too, as the Java plug-in does.
func loadTextFromFile(path string) (string, error) {
	dir, name := filepath.Split(path)
	entries, err := os.ReadDir(dir)
	found := false
	if err == nil {
		for _, e := range entries {
			if e.Name() == name {
				found = true
				break
			}
		}
	}
	if !found {
		return "", errors.New("File not found (case-sensitive): " + path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func concatPath(a, b string) string {
	return a + "/" + b
}

func JdbcSQLByDtoClassRef(ref, sqlRoot string) (string, error) {
	parts := jsplitLit(ref, ":")
	table := "null" // as in Java: 'x:y' without "table:" gives "select * from null"
	switch {
	case len(parts) >= 2:
		if strings.ToLower(strings.TrimSpace(parts[0])) == "table" {
			table = ref[len(parts[0])+1:]
		}
	case IsJdbcStoredProcCall(ref), isSpCallShortcut(ref), isUdfCallShortcut(ref):
		return ref, nil
	case IsSQLFileRef(ref):
		return loadTextFromFile(concatPath(sqlRoot, ref))
	case IsSQLShortcutRef(ref):
		return sqlShortcutToJdbcSQL(ref)
	case IsTableRef(ref):
		table = ref
	default:
		return "", errors.New("Invalid ref: <dto-class ref=\"" + ref + "\"")
	}
	return jdbcSQLByTableName(table), nil
}

func JdbcSQLByRef(ref, sqlRoot string) (string, error) {
	if IsTableRef(ref) {
		return jdbcSQLByTableName(ref), nil
	}
	if IsSQLFileRef(ref) {
		return loadTextFromFile(concatPath(sqlRoot, ref))
	}
	if IsSQLShortcutRef(ref) {
		return sqlShortcutToJdbcSQL(ref)
	}
	return ref, nil
}

func JdbcSQLByExecDmlRef(ref, sqlRoot string) (string, error) {
	if IsSQLFileRef(ref) {
		return loadTextFromFile(concatPath(sqlRoot, ref))
	}
	if IsSQLShortcutRef(ref) {
		return "", errors.New("SQL-shortcuts are not allowed here: ref=\"" + ref + "\"")
	}
	return ref, nil
}

// CRUD ------------------------------------------------------------

func crudCreateSQL(table string, notAI []FieldInfo) (string, error) {
	if len(notAI) == 0 {
		return "", errors.New("Columns not found. Ensure lower/upper case.")
	}
	cols, values := notAI[0].columnName, "?"
	for _, f := range notAI[1:] {
		cols += ", " + f.columnName
		values += ", ?"
	}
	return "insert into " + table + " (" + cols + ") values (" + values + ")", nil
}

func crudReadSQL(table string, pk []FieldInfo, fetchList bool) (string, error) {
	sql := jdbcSQLByTableName(table)
	if !fetchList {
		if len(pk) == 0 {
			return "", errors.New("PK columns not found. Ensure lower/upper case.")
		}
		sql += " where " + pkCondition(pk)
	}
	return sql, nil
}

func pkCondition(pk []FieldInfo) string {
	s := pk[0].columnName + "=?"
	for _, f := range pk[1:] {
		s += " and " + f.columnName + "=?"
	}
	return s
}

func crudUpdateSQL(table string, notPK, pk []FieldInfo) string {
	sql := "update " + table + " set " + notPK[0].columnName + "=?"
	for _, f := range notPK[1:] {
		sql += ", " + f.columnName + "=?"
	}
	return sql + " where " + pkCondition(pk)
}

func crudDeleteSQL(table string, pk []FieldInfo) string {
	return "delete from " + table + " where " + pkCondition(pk)
}

// SQL in code ------------------------------------------------------

func sqlLines(sql string) []string {
	sql = strings.ReplaceAll(sql, "\r", "\n")
	sql = strings.ReplaceAll(sql, "\t", " ")
	return jsplit(sql, reNL)
}

// FormatJdbcSQLForGo puts SQL in backquotes, lines joined by " \n\t\t".
func FormatJdbcSQLForGo(sql string) string {
	var b strings.Builder
	b.WriteString("`")
	for i, line := range sqlLines(sql) {
		if i > 0 {
			b.WriteString(" \n\t\t")
		}
		b.WriteString(line)
	}
	b.WriteString("`")
	return b.String()
}

// FormatJdbcSQLForPython is the SQL inside """...""" (SqlUtils.jdbc_sql_to_python_string): each line is
// escaped by escapeSQLLine, lines joined by " \n" and 16 spaces.
func FormatJdbcSQLForPython(sql string) string {
	var b strings.Builder
	lines := sqlLines(sql)
	for i, line := range lines {
		b.WriteString(escapeSQLLine(line))
		if i < len(lines)-1 {
			b.WriteString(" \n                ")
		}
	}
	return b.String()
}

// FormatJdbcSQLForCpp is the SQL as a C++ string literal with its quotes (SqlUtils.jdbc_sql_to_cpp_str):
// each line escaped by escapeSQLLine, lines joined by ` \n\` and a line break. No lines: only `"`, as in
// Java.
func FormatJdbcSQLForCpp(sql string) string {
	var b strings.Builder
	b.WriteString(`"`)
	lines := sqlLines(sql)
	for i, line := range lines {
		b.WriteString(escapeSQLLine(line))
		if i < len(lines)-1 {
			b.WriteString(" \\n\\\n")
		} else {
			b.WriteString(`"`)
		}
	}
	return b.String()
}

// escapeSQLLine is a line of SQL in Python and C++ code (SqlUtils.escape_sql_line): StringEscapeUtils.escapeJava
// of commons-lang 2.4 (the one in the Velocity jar of the plug-in) for each character, the escapes in upper
// case, '/' as is; a character beyond the BMP is one \UXXXXXXXX, not the two halves of UTF-16 (two lone
// surrogates in Python, an error in C++).
func escapeSQLLine(s string) string {
	var b strings.Builder
	for _, ch := range s {
		switch {
		case ch > 0xffff:
			fmt.Fprintf(&b, `\U%08X`, ch)
		case ch > 0xfff:
			fmt.Fprintf(&b, `\u%X`, ch)
		case ch > 0xff:
			fmt.Fprintf(&b, `\u0%X`, ch)
		case ch > 0x7f:
			fmt.Fprintf(&b, `\u00%X`, ch)
		case ch < 32:
			switch {
			case ch == '\b':
				b.WriteString(`\b`)
			case ch == '\n':
				b.WriteString(`\n`)
			case ch == '\t':
				b.WriteString(`\t`)
			case ch == '\f':
				b.WriteString(`\f`)
			case ch == '\r':
				b.WriteString(`\r`)
			case ch > 0xf:
				fmt.Fprintf(&b, `\u00%X`, ch)
			default:
				fmt.Fprintf(&b, `\u000%X`, ch)
			}
		case ch == '"':
			b.WriteString(`\"`)
		case ch == '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}
