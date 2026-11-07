package cg

import (
	"strings"
	"testing"
)

func vmField() FieldInfo {
	f, _ := NewFieldInfo(AsIs, "gorm-int64", "task_id", "the id")
	return f.WithPK(true).WithAI(false).WithNullable(false).WithColumnSize(10).WithFK("tasks").
		WithIndexed(true).WithRsMd(10, 2, 11).WithDecimalDigits(2).WithAssignFunc("SetInt64")
}

func runVM(t *testing.T, xml string, fi FieldInfo) string {
	t.Helper()
	nodes, err := parseVMXML(xml)
	if err != nil {
		t.Fatalf("%s: %v", xml, err)
	}
	var out strings.Builder
	if err := execVM(nodes, fi, &out); err != nil {
		t.Fatalf("%s: %v", xml, err)
	}
	return out.String()
}

func TestVMBranches(t *testing.T) {
	fi := vmField()
	nodes, err := parseVMXML(`<if var="$fi.PK"><print var="pk"/><else><print var="no"/></else></if><br/>`)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := execVM(nodes, fi, &out); err != nil || out.String() != "pk\n" {
		t.Errorf("pk: %q %v", out.String(), err)
	}
	if got := runVM(t, `<if var="!$fi.PK"><print var="pk"/><else><print var="no"/></else></if>`, fi); got != "no" {
		t.Errorf("else: %q", got)
	}
	if got := runVM(t, `<if var="$fi.Nullable"><print var="a"/><elseif var="$fi.Indexed"><print var="ix"/></elseif></if>`, fi); got != "ix" {
		t.Errorf("elseif: %q", got)
	}
	if got := runVM(t, `<if var="$fi.Nullable"><print var="a"/><elseif var="$fi.Unique"><print var="u"/></elseif></if>`, fi); got != "" {
		t.Errorf("no branch: %q", got)
	}
	// an auto-increment column has no size, as in the plug-in (gorm "size:" is not printed for it)
	if got := runVM(t, `<print var="$fi.ColumnSize"/>`, fi.WithAI(true)); got != "0" {
		t.Errorf("AI size: %q", got)
	}
}

func TestVMExpressions(t *testing.T) {
	fi := vmField()
	for expr, want := range map[string]bool{
		`$fi.PK && $fi.AI`:                   false,
		`$fi.PK and $fi.Nullable`:            false,
		`$fi.Nullable || $fi.Indexed`:        true,
		`$fi.Nullable or $fi.Unique`:         false,
		`!($fi.Nullable || $fi.Unique)`:      true,
		`not $fi.PK`:                         false,
		`$fi.ColumnSize == 10`:               true,
		`$fi.ColumnSize eq 10`:               true,
		`$fi.ColumnSize != 10`:               false,
		`$fi.ColumnSize ne 9`:                true,
		`$fi.ColumnSize > 9`:                 true,
		`$fi.ColumnSize gt 10`:               false,
		`$fi.ColumnSize < 11`:                true,
		`$fi.ColumnSize lt 10`:               false,
		`$fi.ColumnSize >= 10`:               true,
		`$fi.Precision ge 11`:                false,
		`$fi.Scale <= 2`:                     true,
		`$fi.DisplaySize le 10`:              false,
		`$fi.DecimalDigits == -1`:            false,
		`$fi.Name == "task_id"`:              true,
		`$fi.ColumnName eq 'task_id'`:        true,
		`$fi.Name != "x"`:                    true,
		`$fi.Model == "gorm"`:                true,
		`$fi.FK == "tasks"`:                  true,
		`$fi.Comment == "the id"`:            true,
		`$fi.Unknown == $fi.Nothing`:         true,
		`$fi.Unknown != $fi.Nothing`:         false,
		`$fi.Name > "a"`:                     false,
		`true`:                               true,
		`false`:                              false,
		`"text"`:                             true,
		`$fi.Unknown`:                        false,
		`${fi.PK}`:                           true,
		`$!fi.PK`:                            true,
		`$fi.Type == "gorm-int64"`:           true,
		`$fi.AssignFunc == "SetInt64"`:       true,
		`$fi.LowerCamelCaseName == "taskID"`: true,
		`$fi.OriginalType == "gorm-int64"`:   true,
		`$fi.ScalarType == "int64"`:          true,
		`$fi.OriginalScalarType == "int64"`:  true,
	} {
		got, err := evalBool(expr, fi)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if got != want {
			t.Errorf("%s: %t, want %t", expr, got, want)
		}
	}
	plain, _ := NewFieldInfo(AsIs, "int64", "id", "")
	if ok, err := evalBool(`$fi.Model`, plain); ok || err != nil {
		t.Errorf("no model: %t %v", ok, err)
	}
	if ok, err := evalBool(`$fi.FK`, plain); ok || err != nil {
		t.Errorf("no fk: %t %v", ok, err)
	}
}

func TestVMErrors(t *testing.T) {
	fi := vmField()
	for _, expr := range []string{
		`$fi.PK &&`,
		`($fi.PK`,
		`"open`,
		`$fi.PK $fi.AI`,
		`$x.PK`,
		`abc`,
		`$fi.PK == `,
		`#`,
		``,
	} {
		if _, err := evalBool(expr, fi); err == nil {
			t.Errorf("%q: no error", expr)
		}
	}
	for _, xml := range []string{
		`<foreach var="x"/>`,
		`<if var="$fi.PK">`,
		`<if var="$fi.PK"></else></if>`,
	} {
		if _, err := parseVMXML(xml); err == nil {
			t.Errorf("%q: no parse error", xml)
		}
	}
	// a bad expression is reported at execution, in any branch
	for _, xml := range []string{
		`<if var="$fi.PK &amp;&amp;"><print var="x"/></if>`,
		`<if var="false"/><elseif var="(("/>`,
		`<if var="true"><if var="(("/></if>`,
		`<if var="false"><else><if var="(("/></else></if>`,
		`<if var="false"><elseif var="true"><if var="(("/></elseif></if>`,
		`<else/>`,
	} {
		nodes, err := parseVMXML(xml)
		if err != nil {
			t.Errorf("%q: %v", xml, err)
			continue
		}
		var out strings.Builder
		if err := execVM(nodes, fi, &out); err == nil {
			t.Errorf("%q: no exec error", xml)
		}
	}
}

func TestInterpolate(t *testing.T) {
	fi := vmField()
	for in, want := range map[string]string{
		`size:$fi.ColumnSize`:     "size:10",
		`${fi.Name};`:             "task_id;",
		`$!fi.Unknown|`:           "|",
		`$fi.Unknown|`:            "$fi.Unknown|",
		`${fi.Name`:               "${fi.Name",
		`${column} ${0}`:          "${column} ${0}",
		`$fi.PK/$fi.AI/$fi.Model`: "true/false/gorm",
	} {
		if got := interpolate(in, fi); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestFieldAccessors(t *testing.T) {
	f := vmField().WithAI(true).WithName("TaskID").WithScalarType("int64")
	if f.Name() != "TaskID" || f.LowerCamelName() != "taskID" || !f.IsAI() || !f.IsPK() ||
		f.OriginalType() != "gorm-int64" || f.ScalarType() != "int64" || f.OriginalScalarType() != "int64" ||
		f.Precision() != 10 || f.Scale() != 2 {
		t.Errorf("accessors: %+v", f)
	}
	if got := f.withModel("sqlx").model; got != "gorm" {
		t.Errorf("withModel keeps the model of the type: %q", got)
	}
	plain, _ := NewFieldInfo(AsIs, "int64", "id", "")
	if got := plain.withModel("sqlx").model; got != "sqlx" {
		t.Errorf("withModel: %q", got)
	}
	if _, err := NewFieldInfo(AsIs, "a-b-c", "id", ""); err == nil {
		t.Error("a-b-c: no error")
	}
	if ex, _ := NewFieldInfo(AsIs, "-", "id", ""); ex.originalScalarType != "-" {
		t.Errorf("excluded: %+v", ex)
	}
}
