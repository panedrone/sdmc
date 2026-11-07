package mysql

import (
	"errors"
	"testing"

	gm "github.com/go-mysql-org/go-mysql/mysql"
)

func TestParamJavaType(t *testing.T) {
	for _, c := range []struct{ dataType, dtd, want string }{
		{"tinyint", "tinyint(1)", "java.lang.Boolean"},
		{"tinyint", "tinyint", "java.lang.Integer"},
		{"smallint", "smallint", "java.lang.Integer"},
		{"mediumint", "mediumint", "java.lang.Integer"},
		{"int", "int", "java.lang.Integer"},
		{"int", "int unsigned", "java.lang.Long"},
		{"bigint", "bigint", "java.lang.Long"},
		{"bigint", "bigint unsigned", "java.math.BigInteger"},
		{"decimal", "decimal(10,2)", "java.math.BigDecimal"},
		{"float", "float", "java.lang.Float"},
		{"double", "double", "java.lang.Double"},
		{"bit", "bit(1)", "java.lang.Boolean"},
		{"datetime", "datetime", "java.time.LocalDateTime"},
		{"date", "date", "java.util.Date"},
		{"year", "year", "java.util.Date"},
		{"varchar", "varchar(10)", "java.lang.String"},
		{"json", "json", "java.lang.String"},
		{"blob", "blob", "byte[]"},
		{"geometry", "geometry", "byte[]"},
		{"vector", "vector", "java.lang.Object"},
	} {
		if got := paramJavaType(c.dataType, c.dtd); got != c.want {
			t.Errorf("paramJavaType(%q, %q) = %q, want %q", c.dataType, c.dtd, got, c.want)
		}
	}
}

func TestParamRawName(t *testing.T) {
	if got := paramRawName("int", "int unsigned"); got != "UNSIGNED INT" {
		t.Errorf("got %q", got)
	}
	if got := paramRawName("varchar", "varchar(10)"); got != "VARCHAR" {
		t.Errorf("got %q", got)
	}
}

func TestRawName(t *testing.T) {
	const utf8 = 255
	for _, c := range []struct {
		f    field
		want string
	}{
		{field{typ: tTiny}, "TINYINT"},
		{field{typ: tTiny, flags: flagUnsigned}, "UNSIGNED TINYINT"},
		{field{typ: tShort}, "SMALLINT"},
		{field{typ: tInt24}, "MEDIUMINT"},
		{field{typ: tLong, flags: flagUnsigned}, "UNSIGNED INT"},
		{field{typ: tLongLong}, "BIGINT"},
		{field{typ: tNewDecimal}, "DECIMAL"},
		{field{typ: tDecimal}, "DECIMAL"},
		{field{typ: tFloat}, "FLOAT"},
		{field{typ: tDouble}, "DOUBLE"},
		{field{typ: tNull}, "NULL"},
		{field{typ: tTimestamp2}, "TIMESTAMP"},
		{field{typ: tNewDate}, "DATE"},
		{field{typ: tTime2}, "TIME"},
		{field{typ: tDateTime2}, "DATETIME"},
		{field{typ: tYear}, "YEAR"},
		{field{typ: tBit}, "BIT"},
		{field{typ: tJSON}, "JSON"},
		{field{typ: tGeometry}, "GEOMETRY"},
		{field{typ: tEnum}, "ENUM"},
		{field{typ: tSet}, "SET"},
		{field{typ: tVarString, charset: utf8}, "VARCHAR"},
		{field{typ: tVarString, charset: binaryCharset}, "VARBINARY"},
		{field{typ: tVarChar, flags: flagEnum}, "ENUM"},
		{field{typ: tVarChar, flags: flagSet}, "SET"},
		{field{typ: tString, charset: utf8}, "CHAR"},
		{field{typ: tString, charset: binaryCharset}, "BINARY"},
		{field{typ: tString, flags: flagEnum}, "ENUM"},
		{field{typ: tString, flags: flagSet}, "SET"},
		{field{typ: tTinyBlob, charset: binaryCharset}, "TINYBLOB"},
		{field{typ: tTinyBlob, charset: utf8}, "TINYTEXT"},
		{field{typ: tMediumBlob, charset: binaryCharset}, "MEDIUMBLOB"},
		{field{typ: tMediumBlob, charset: utf8}, "MEDIUMTEXT"},
		{field{typ: tLongBlob, charset: binaryCharset}, "LONGBLOB"},
		{field{typ: tLongBlob, charset: utf8}, "LONGTEXT"},
		{field{typ: tBlob, charset: binaryCharset}, "BLOB"},
		{field{typ: tBlob, charset: utf8}, "TEXT"},
		{field{typ: 200}, ""},
	} {
		if got := c.f.rawName(); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.f, got, c.want)
		}
	}
}

func TestColumnSQLType(t *testing.T) {
	for _, c := range []struct {
		dataType, columnType string
		want                 int
	}{
		{"tinyint", "tinyint(1)", -7},
		{"tinyint", "tinyint", -6},
		{"smallint", "", 5},
		{"int", "", 4},
		{"bigint", "", -5},
		{"decimal", "", 3},
		{"float", "", 7},
		{"double", "", 8},
		{"boolean", "", -7},
		{"year", "", 91},
		{"timestamp", "", 93},
		{"time", "", 92},
		{"enum", "", 1},
		{"tinytext", "", 12},
		{"json", "", -1},
		{"point", "", -2},
		{"tinyblob", "", -3},
		{"longblob", "", -4},
		{"vector", "", 1111},
	} {
		if got := (column{dataType: c.dataType, columnType: c.columnType}).sqlType(); got != c.want {
			t.Errorf("%s %s: got %d, want %d", c.dataType, c.columnType, got, c.want)
		}
	}
}

func TestSplitSchemaAndSchemaExpr(t *testing.T) {
	if s, tb, err := splitSchema("t"); s != "" || tb != "t" || err != nil {
		t.Errorf("t: %q %q %v", s, tb, err)
	}
	if s, tb, err := splitSchema("db.t"); s != "db" || tb != "t" || err != nil {
		t.Errorf("db.t: %q %q %v", s, tb, err)
	}
	if _, _, err := splitSchema("a.b.c"); err == nil {
		t.Error("a.b.c: no error")
	}
	var args []any
	if got := schemaExpr("", &args); got != "DATABASE()" || len(args) != 0 {
		t.Errorf("empty: %q %v", got, args)
	}
	if got := schemaExpr("db", &args); got != "?" || len(args) != 1 || args[0] != "db" {
		t.Errorf("db: %q %v", got, args)
	}
}

func TestMaxLenAndCatalog(t *testing.T) {
	db := &DB{mbMaxLen: map[uint16]int{255: 4}}
	if got := db.maxLen(binaryCharset); got != 1 {
		t.Errorf("binary: %d", got)
	}
	if got := db.maxLen(255); got != 4 {
		t.Errorf("utf8mb4: %d", got)
	}
	if got := db.IdentifierQuote(); got != "`" {
		t.Errorf("quote %q", got)
	}
	if got := db.RawOfJava(" java.lang.Integer "); got == "" {
		t.Error("RawOfJava: empty for java.lang.Integer")
	}
	if got := db.RawOfJava("x.Unknown"); got != "" {
		t.Errorf("RawOfJava unknown: %q", got)
	}
}

func TestServerMessage(t *testing.T) {
	if got := serverMessage(&gm.MyError{Code: 1146, Message: "Table 'x' doesn't exist"}); got != "Table 'x' doesn't exist" {
		t.Errorf("MyError: %q", got)
	}
	if got := serverMessage(errors.New("io")); got != "io" {
		t.Errorf("other: %q", got)
	}
}
