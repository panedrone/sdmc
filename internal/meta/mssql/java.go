package mssql

import "strings"

// javaRaw maps the plug-in's Java names to the system types of SQL Server.
var javaRaw = map[string]string{
	"java.lang.Boolean":            "bit",
	"java.lang.Byte":               "tinyint",
	"java.lang.Short":              "smallint",
	"java.lang.Integer":            "int",
	"java.lang.Long":               "bigint",
	"java.lang.Float":              "real",
	"java.lang.Double":             "float",
	"java.math.BigDecimal":         "decimal",
	"java.math.BigInteger":         "decimal",
	"java.lang.String":             "nvarchar",
	"java.util.Date":               "datetime",
	"java.sql.Timestamp":           "datetime",
	"java.time.LocalDateTime":      "datetime2",
	"java.sql.Date":                "date",
	"java.time.LocalDate":          "date",
	"java.sql.Time":                "time",
	"microsoft.sql.DateTimeOffset": "datetimeoffset",
	"byte[]":                       "varbinary",
	"java.util.UUID":               "uniqueidentifier",
}

func (db *DB) RawOfJava(javaType string) string { return javaRaw[strings.TrimSpace(javaType)] }
