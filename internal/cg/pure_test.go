package cg

import (
	"strings"
	"testing"
)

func TestEscapeControls(t *testing.T) {
	if got, want := FormatJdbcSQLForPython("select '\b\f\u20ac'"), `select '\b\f\`+"u20AC'"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
	if got := escapeSQLLine("a\tb\nc\rd"); got != `a\tb\nc\rd` {
		t.Errorf("%q", got)
	}
}

func TestRefs(t *testing.T) {
	for ref, want := range map[string]bool{"": false, "tasks": true, "(tasks": false, "(tasks)": false, "dbo.tasks": true} {
		if got := IsTableRef(ref); got != want {
			t.Errorf("IsTableRef(%q) = %t", ref, got)
		}
	}
	for ref, want := range map[string]bool{"{call p(?)}": true, "{call p}": true, "{call p(?)": false, "select 1": false} {
		if got := IsJdbcStoredProcCall(ref); got != want {
			t.Errorf("IsJdbcStoredProcCall(%q) = %t", ref, got)
		}
	}
	if _, err := parseSQLShortcutRef("(g_id)"); err == nil {
		t.Error("(g_id): no error")
	}
	if _, err := parseSQLShortcutRef("t(a)/b/c"); err == nil {
		t.Error("two '/': no error")
	}
	if got, err := sqlShortcutToJdbcSQL("tasks()"); err != nil || got != "select * from tasks" {
		t.Errorf("tasks(): %q %v", got, err)
	}
	if _, err := sqlShortcutToJdbcSQL("tasks(a,,b)"); err == nil {
		t.Error("tasks(a,,b): no error")
	}
	if got, err := JdbcSQLByDtoClassRef("tasks", ""); err != nil || got != "select * from tasks" {
		t.Errorf("table ref: %q %v", got, err)
	}
	if got, err := JdbcSQLByDtoClassRef("table:dbo.tasks", ""); err != nil || !strings.Contains(got, "dbo.tasks") {
		t.Errorf("table: ref: %q %v", got, err)
	}
	if got, err := JdbcSQLByRef("tasks", ""); err != nil || got != "select * from tasks" {
		t.Errorf("JdbcSQLByRef: %q %v", got, err)
	}
	if _, err := JdbcSQLByExecDmlRef("tasks(a)", ""); err == nil {
		t.Error("exec-dml shortcut: no error")
	}
	if _, err := crudCreateSQL("t", nil); err == nil {
		t.Error("crudCreateSQL without columns: no error")
	}
	if _, err := crudReadSQL("t", nil, false); err == nil {
		t.Error("crudReadSQL without PK: no error")
	}
}

func TestDeclarations(t *testing.T) {
	if _, _, err := ParseMethodDeclaration("m(a)b"); err == nil {
		t.Error("m(a)b: no error")
	}
	if _, _, err := ParseMethodDeclaration("m(a"); err == nil {
		t.Error("m(a: no error")
	}
	for list, ok := range map[string]bool{"a, b": true, "a,,b": false, "a:b": false, "a#b": false} {
		_, err := ListedItems(list, false)
		if (err == nil) != ok {
			t.Errorf("ListedItems(%q): %v", list, err)
		}
	}
	if _, err := ListedItems("on_t:Task", true); err != nil {
		t.Errorf("colon allowed: %v", err)
	}
	if err := checkItem("", false); err == nil {
		t.Error("empty item: no error")
	}
	if _, err := ParseExecDmlParams([]string{"[a, b]"}); err == nil {
		t.Error("cursors without types: no error")
	}
	if _, err := ParseExecDmlParams([]string{"[a,,b]"}); err == nil {
		t.Error("cursors, empty item: no error")
	}
}

func TestNamesMore(t *testing.T) {
	for in, want := range map[string]string{"[EmployeeID] ASC": "employeeid", "x desc": "", "id": "id"} {
		got, err := pkColNameAlias(in)
		if in == "x desc" {
			if err == nil {
				t.Errorf("%q: no error", in)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if got, err := pkColNameAlias("a desc b desc"); err != nil || got != "b" {
		t.Errorf("desc: %q %v", got, err)
	}
	if LowerCamel("ID") != "id" || Title("") != "" {
		t.Errorf("LowerCamel(ID) = %q, Title(\"\") = %q", LowerCamel("ID"), Title(""))
	}
}
