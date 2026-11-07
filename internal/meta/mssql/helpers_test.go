package mssql

import (
	"errors"
	"reflect"
	"testing"

	mssqldb "github.com/microsoft/go-mssqldb"
)

func TestScalars(t *testing.T) {
	for v, want := range map[any]int{int64(7): 7, int32(6): 6, int16(5): 5, uint8(4): 4, 3: 3, "x": 0} {
		if got := toInt(v); got != want {
			t.Errorf("toInt(%#v) = %d", v, got)
		}
	}
	if toInt([]byte("12")) != 12 {
		t.Error("toInt []byte")
	}
	if !toBool(true) || !toBool(int64(1)) || toBool(int64(0)) || toBool("1") {
		t.Error("toBool")
	}
	if toString("a") != "a" || toString([]byte("b")) != "b" || toString(1) != "" {
		t.Error("toString")
	}
	for in, want := range map[string]string{"nvarchar(15)": "nvarchar", " Decimal (10,2)": "decimal", "INT": "int"} {
		if got := baseType(in); got != want {
			t.Errorf("baseType(%q) = %q", in, got)
		}
	}
}

func TestDeclaration(t *testing.T) {
	for _, c := range []struct {
		t    colType
		want string
	}{
		{colType{name: "nvarchar", maxLength: 30}, "nvarchar(15)"},
		{colType{name: "varchar", maxLength: -1}, "varchar(max)"},
		{colType{name: "varbinary", maxLength: 8}, "varbinary(8)"},
		{colType{name: "decimal", precision: 10, scale: 2}, "decimal(10,2)"},
		{colType{name: "datetime2", scale: 7}, "datetime2(7)"},
		{colType{name: "int"}, "int"},
	} {
		if got := c.t.declaration(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.t, got, c.want)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	if splitArgs("") != nil {
		t.Error("empty")
	}
	got := splitArgs(`@P1, 'a,b', [x,y], "c,d" ,2`)
	want := []string{"@P1", "'a,b'", "[x,y]", `"c,d"`, "2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSplitSchemaNullable(t *testing.T) {
	if s, tb, err := splitSchema("dbo.t"); s != "dbo" || tb != "t" || err != nil {
		t.Errorf("dbo.t: %q %q %v", s, tb, err)
	}
	if s, tb, err := splitSchema("t"); s != "" || tb != "t" || err != nil {
		t.Errorf("t: %q %q %v", s, tb, err)
	}
	if _, _, err := splitSchema("a.b.c"); err == nil {
		t.Error("a.b.c: no error")
	}
	if nullable("") != nil || nullable("dbo") != "dbo" {
		t.Error("nullable")
	}
}

func TestMoreTypes(t *testing.T) {
	for _, c := range []struct {
		t          colType
		java       string
		size, sql  int
		clr, ucode bool
	}{
		{colType{name: "bit"}, "java.lang.Boolean", 1, -7, false, false},
		{colType{name: "tinyint"}, "java.lang.Short", 3, -6, false, false},
		{colType{name: "bigint"}, "java.lang.Long", 19, -5, false, false},
		{colType{name: "numeric", precision: 5}, "java.math.BigDecimal", 5, 2, false, false},
		{colType{name: "smallmoney"}, "java.math.BigDecimal", 10, 3, false, false},
		{colType{name: "date"}, "java.util.Date", 10, 91, false, false},
		{colType{name: "smalldatetime"}, "java.util.Date", 16, 93, false, false},
		{colType{name: "char", maxLength: 5}, "java.lang.String", 5, 1, false, false},
		{colType{name: "nchar", maxLength: 10}, "java.lang.String", 5, -15, false, true},
		{colType{name: "text"}, "java.lang.String", maxBytes, -1, false, false},
		{colType{name: "binary", maxLength: 4}, "byte[]", 4, -2, false, false},
		{colType{name: "image"}, "byte[]", maxBytes, -4, false, false},
		{colType{name: "geometry"}, "byte[]", maxBytes, -3, true, false},
		{colType{name: "hierarchyid", maxLength: 892}, "byte[]", 892, -3, true, false},
		{colType{name: "time", scale: 0}, "java.util.Date", 8, 92, false, false},
		{colType{name: "cursor"}, "java.lang.Object", 0, 1111, false, false},
	} {
		if got := c.t.javaType(); got != c.java {
			t.Errorf("%s: java %s, want %s", c.t.name, got, c.java)
		}
		if got := c.t.size(); got != c.size {
			t.Errorf("%s: size %d, want %d", c.t.name, got, c.size)
		}
		if got := c.t.sqlType(); got != c.sql {
			t.Errorf("%s: sql %d, want %d", c.t.name, got, c.sql)
		}
		if c.t.clr() != c.clr || c.t.unicode() != c.ucode {
			t.Errorf("%s: clr %t unicode %t", c.t.name, c.t.clr(), c.t.unicode())
		}
	}
	if got := (colType{name: "varbinary", maxLength: -1}).displaySize(); got != maxBytes {
		t.Errorf("varbinary(max) display %d", got)
	}
}

func TestCatalogAndMessages(t *testing.T) {
	db := &DB{}
	if db.IdentifierQuote() != "[" {
		t.Error("quote")
	}
	if db.RawOfJava(" java.lang.Integer ") == "" || db.RawOfJava("x.Unknown") != "" {
		t.Error("RawOfJava")
	}
	e := mssqldb.Error{Message: "summary", All: []mssqldb.Error{{Message: "cause"}}}
	if got := serverMessage(e); got != "cause" {
		t.Errorf("All: %q", got)
	}
	if got := serverMessage(mssqldb.Error{Message: "only"}); got != "only" {
		t.Errorf("Message: %q", got)
	}
	if got := serverMessage(errors.New("io")); got != "io" {
		t.Errorf("other: %q", got)
	}
}
