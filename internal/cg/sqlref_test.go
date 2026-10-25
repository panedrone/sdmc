package cg

import "testing"

// SQL in Python code: as SqlUtils.jdbc_sql_to_python_string, escapeJava of commons-lang 2.4 for each
// character, a character beyond the BMP as one \U.
func TestFormatJdbcSQLForPython(t *testing.T) {
	for sql, want := range map[string]string{
		"select * from t":                    "select * from t",
		"select *\r\n\tfrom t\n\n where a=?": "select * \n                 from t \n                 where a=?",
		`select 'a\b "c" d/e' from t`:        `select 'a\\b \"c\" d/e' from t`,
		"select 'é ∑ \U0001F600'":            `select '\u00E9 \u2211 \U0001F600'`,
		"select '\x01\x1f'":                  `select '\u0001\u001F'`,
		"select 'http://x//y', 'a\\/b'":      `select 'http://x//y', 'a\\/b'`,
	} {
		if got := FormatJdbcSQLForPython(sql); got != want {
			t.Errorf("%q:\n got  %q\n want %q", sql, got, want)
		}
	}
}

// SQL in C++ code: the same escapes in a string literal, lines continued by ` \n\`.
func TestFormatJdbcSQLForCpp(t *testing.T) {
	for sql, want := range map[string]string{
		"select * from t":             `"select * from t"`,
		"select *\nfrom t":            "\"select * \\n\\\nfrom t\"",
		"select '\U0001F600 é \"q\"'": `"select '\U0001F600 \u00E9 \"q\"'"`,
	} {
		if got := FormatJdbcSQLForCpp(sql); got != want {
			t.Errorf("%q:\n got  %q\n want %q", sql, got, want)
		}
	}
}
