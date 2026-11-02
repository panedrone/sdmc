package mssql

import "testing"

func TestToNative(t *testing.T) {
	for _, c := range []struct {
		jdbc, native string
		n            int
		proc         string
	}{
		{"select * from t where a = ? and b = ?", "select * from t where a = @P1 and b = @P2", 2, ""},
		{"select '?' as q, [a?b], \"c?\" from t where x = ? -- ?\n and y = 1", "select '?' as q, [a?b], \"c?\" from t where x = @P1 -- ?\n and y = 1", 1, ""},
		{"select 'it''s ?' from t /* ? */ where y = ?", "select 'it''s ?' from t /* ? */ where y = @P1", 1, ""},
		{"{call CustOrderHist(?)}", "EXEC CustOrderHist @P1", 1, "CustOrderHist"},
		{"{call [Sales by Year](?, ?)}", "EXEC [Sales by Year] @P1, @P2", 2, "[Sales by Year]"},
		{"{call [Ten Most Expensive Products]}", "EXEC [Ten Most Expensive Products]", 0, "[Ten Most Expensive Products]"},
		{"exec dbo.CustOrderHist ?", "EXEC dbo.CustOrderHist @P1", 1, "dbo.CustOrderHist"},
		{"select count(*) from Customers", "select count(*) from Customers", 0, ""},
	} {
		native, n, proc := toNative(c.jdbc)
		if native != c.native || n != c.n || proc != c.proc {
			t.Errorf("%s:\n got  %q %d %q\n want %q %d %q", c.jdbc, native, n, proc, c.native, c.n, c.proc)
		}
	}
}

func TestConnString(t *testing.T) {
	for _, c := range []struct{ url, user, pwd, want string }{
		{"sqlserver://localhost:1433?database=Northwind", "sa", "p", "sqlserver://sa:p@localhost:1433?database=Northwind"},
		{"sqlserver://u:x@host/SQLEXPRESS?database=db", "sa", "p", "sqlserver://u:x@host/SQLEXPRESS?database=db"},
		{"jdbc:sqlserver://localhost:1433;databaseName=Northwind;encrypt=false", "sa", "p",
			"sqlserver://sa:p@localhost:1433?TrustServerCertificate=true&database=Northwind&encrypt=false"},
		{"sqlserver://localhost:1433;databaseName=Northwind;encrypt=false;trustServerCertificate=true", "sa", "p",
			"sqlserver://sa:p@localhost:1433?TrustServerCertificate=true&database=Northwind&encrypt=false"},
		{`jdbc:sqlserver://localhost\SQLEXPRESS:1433;databaseName=db`, "sa", "p", "sqlserver://sa:p@localhost:1433/SQLEXPRESS?database=db"},
		{"jdbc:sqlserver://localhost;instanceName=SQLEXPRESS;databaseName=db;integratedSecurity=true", "sa", "p",
			"sqlserver://localhost/SQLEXPRESS?database=db"},
	} {
		got, err := ConnString(c.url, c.user, c.pwd)
		if err != nil || got != c.want {
			t.Errorf("%s:\n got  %q %v\n want %q", c.url, got, err, c.want)
		}
	}
	if _, err := ConnString("postgres://localhost/db", "", ""); err == nil {
		t.Error("not SQL Server: an error expected")
	}
}

// The rules of mssql-jdbc, as metadump shows them against MetaDump.java (cli/metadump/mssql_*.txt).
func TestTypes(t *testing.T) {
	for _, c := range []struct {
		t                                            colType
		java                                         string
		precision, scale, display, size, sql, digits int
	}{
		{colType{name: "int"}, "java.lang.Integer", 10, 0, 11, 10, 4, 0},
		{colType{name: "smallint"}, "java.lang.Short", 5, 0, 6, 5, 5, 0},
		{colType{name: "money"}, "java.math.BigDecimal", 19, 4, 21, 19, 3, 4},
		{colType{name: "decimal", precision: 10, scale: 2}, "java.math.BigDecimal", 10, 2, 12, 10, 3, 2},
		{colType{name: "real"}, "java.lang.Float", 7, 0, 13, 24, 7, 0},
		{colType{name: "float"}, "java.lang.Double", 15, 0, 22, 53, 8, 0},
		{colType{name: "nvarchar", maxLength: 30}, "java.lang.String", 15, 0, 15, 15, -9, 0},
		{colType{name: "nvarchar", maxLength: -1}, "java.lang.String", maxChars, 0, maxChars, maxBytes, -9, 0},
		{colType{name: "varchar", maxLength: -1}, "java.lang.String", maxBytes, 0, maxBytes, maxBytes, 12, 0},
		{colType{name: "ntext", maxLength: 16}, "java.lang.String", maxChars, 0, maxChars, maxChars, -16, 0},
		{colType{name: "xml", maxLength: -1}, "java.lang.String", maxChars, 0, maxChars, maxBytes, -16, 0},
		{colType{name: "varbinary", maxLength: 10}, "byte[]", 10, 0, 20, 10, -3, 0},
		{colType{name: "timestamp", maxLength: 8}, "byte[]", 8, 0, 16, 8, -2, 0},
		{colType{name: "datetime"}, "java.util.Date", 23, 3, 23, 23, 93, 3},
		{colType{name: "datetime2", scale: 7}, "java.util.Date", 27, 7, 27, 27, 93, 7},
		{colType{name: "time", scale: 3}, "java.util.Date", 12, 3, 12, 12, 92, 3},
		{colType{name: "datetimeoffset", scale: 7}, "microsoft.sql.DateTimeOffset", 34, 7, 34, 34, -155, 7},
		{colType{name: "uniqueidentifier"}, "java.lang.String", 36, 0, 36, 36, 1, 0},
		{colType{name: "sql_variant"}, "java.lang.Object", 0, 0, 0, maxBytes, -150, 0},
		// CLR types: varbinary for mssql-jdbc
		{colType{name: "geography", maxLength: -1}, "byte[]", maxBytes, 0, maxBytes, maxBytes, -3, 0},
		{colType{name: "geometry", maxLength: -1}, "byte[]", maxBytes, 0, maxBytes, maxBytes, -3, 0},
		{colType{name: "hierarchyid", maxLength: 892}, "byte[]", 892, 0, 1784, 892, -3, 0},
	} {
		got := []int{c.t.rsPrecision(), c.t.rsScale(), c.t.displaySize(), c.t.columnSize(), c.t.sqlType(), c.t.decimalDigits()}
		want := []int{c.precision, c.scale, c.display, c.size, c.sql, c.digits}
		if c.t.javaType() != c.java {
			t.Errorf("%s: java %s, want %s", c.t.name, c.t.javaType(), c.java)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%+v: precision, scale, display, size, sql, digits %v, want %v", c.t, got, want)
				break
			}
		}
	}
}
