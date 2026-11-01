package mssql

import "github.com/panedrone/sdmc/internal/meta"

// colType is a column type as sys.columns and sp_describe_first_result_set give it.
type colType struct {
	name      string // the system type in lower case: int, nvarchar, datetime2, ...
	maxLength int    // bytes; -1 for (max)
	precision int
	scale     int
}

// java.sql.Types of mssql-jdbc DatabaseMetaData.getColumns.
const (
	tBit            = -7
	tTinyint        = -6
	tBigint         = -5
	tLongVarbinary  = -4
	tVarbinary      = -3
	tBinary         = -2
	tLongVarchar    = -1
	tChar           = 1
	tNumeric        = 2
	tDecimal        = 3
	tInteger        = 4
	tSmallint       = 5
	tReal           = 7
	tDouble         = 8
	tVarchar        = 12
	tDate           = 91
	tTime           = 92
	tTimestamp      = 93
	tNChar          = -15
	tNVarchar       = -9
	tLongNVarchar   = -16
	tDateTimeOffset = -155 // microsoft.sql.Types.DATETIMEOFFSET
	tSQLVariant     = -150 // as getColumns of mssql-jdbc reports sql_variant
	tOther          = 1111
)

const (
	maxBytes = 2147483647 // varchar(max), varbinary(max), text, image
	maxChars = 1073741823 // nvarchar(max), ntext, xml
)

// javaType is getColumnClassName of mssql-jdbc as the plug-in refines it (java.sql date-time -> java.util.Date).
func (t colType) javaType() string {
	switch t.name {
	case "bit":
		return "java.lang.Boolean"
	case "tinyint", "smallint":
		return "java.lang.Short"
	case "int":
		return "java.lang.Integer"
	case "bigint":
		return "java.lang.Long"
	case "decimal", "numeric", "money", "smallmoney":
		return "java.math.BigDecimal"
	case "real":
		return "java.lang.Float"
	case "float":
		return "java.lang.Double"
	case "char", "varchar", "text", "nchar", "nvarchar", "ntext", "xml", "uniqueidentifier":
		return "java.lang.String"
	case "date", "time", "datetime", "datetime2", "smalldatetime":
		return "java.util.Date"
	case "datetimeoffset":
		return "microsoft.sql.DateTimeOffset"
	case "binary", "varbinary", "image", "timestamp":
		return "byte[]"
	}
	return meta.JavaObject
}

func (t colType) unicode() bool {
	return t.name == "nchar" || t.name == "nvarchar"
}

// length is the length of a character or binary type: characters for nchar/nvarchar, bytes else.
func (t colType) length() int {
	if t.maxLength < 0 {
		if t.unicode() {
			return maxChars
		}
		return maxBytes
	}
	if t.unicode() {
		return t.maxLength / 2
	}
	return t.maxLength
}

// fractional is the size of the fractional seconds of time, datetime2 and datetimeoffset: ".nnnnnnn".
func (t colType) fractional() int {
	if t.scale > 0 {
		return t.scale + 1
	}
	return 0
}

// size is the precision of ResultSetMetaData and the COLUMN_SIZE of getColumns, apart from real and float.
func (t colType) size() int {
	switch t.name {
	case "bit":
		return 1
	case "tinyint":
		return 3
	case "smallint":
		return 5
	case "int":
		return 10
	case "bigint":
		return 19
	case "decimal", "numeric":
		return t.precision
	case "money":
		return 19
	case "smallmoney":
		return 10
	case "date":
		return 10
	case "time":
		return 8 + t.fractional()
	case "datetime":
		return 23
	case "smalldatetime":
		return 16
	case "datetime2":
		return 19 + t.fractional()
	case "datetimeoffset":
		return 26 + t.fractional()
	case "uniqueidentifier":
		return 36
	case "char", "varchar", "nchar", "nvarchar", "binary", "varbinary":
		return t.length()
	case "text", "image":
		return maxBytes
	case "ntext", "xml":
		return maxChars
	case "timestamp":
		return 8
	}
	return 0
}

// rsPrecision is ResultSetMetaData.getPrecision.
func (t colType) rsPrecision() int {
	switch t.name {
	case "real":
		return 7
	case "float":
		return 15
	}
	return t.size()
}

// rsScale is ResultSetMetaData.getScale.
func (t colType) rsScale() int {
	switch t.name {
	case "decimal", "numeric", "time", "datetime2", "datetimeoffset":
		return t.scale
	case "money", "smallmoney":
		return 4
	case "datetime":
		return 3
	}
	return 0
}

// displaySize is ResultSetMetaData.getColumnDisplaySize.
func (t colType) displaySize() int {
	switch t.name {
	case "smallint":
		return 6
	case "int":
		return 11
	case "bigint":
		return 20
	case "decimal", "numeric", "money", "smallmoney":
		return t.size() + 2
	case "real":
		return 13
	case "float":
		return 22
	case "binary", "varbinary":
		if t.maxLength < 0 {
			return maxBytes
		}
		return 2 * t.maxLength
	case "timestamp": // rowversion: 8 bytes in hex
		return 16
	}
	return t.rsPrecision()
}

// columnSize is COLUMN_SIZE of getColumns: (max), xml and sql_variant are 2147483647 there, nvarchar(max) too.
func (t colType) columnSize() int {
	switch t.name {
	case "real":
		return 24
	case "float":
		return 53
	case "xml", "sql_variant":
		return maxBytes
	case "varchar", "nvarchar", "varbinary":
		if t.maxLength < 0 {
			return maxBytes
		}
	}
	return t.size()
}

// decimalDigits is DECIMAL_DIGITS of getColumns.
func (t colType) decimalDigits() int {
	return t.rsScale()
}

// sqlType is DATA_TYPE of getColumns.
func (t colType) sqlType() int {
	switch t.name {
	case "bit":
		return tBit
	case "tinyint":
		return tTinyint
	case "smallint":
		return tSmallint
	case "int":
		return tInteger
	case "bigint":
		return tBigint
	case "decimal", "money", "smallmoney":
		return tDecimal
	case "numeric":
		return tNumeric
	case "real":
		return tReal
	case "float":
		return tDouble
	case "char", "uniqueidentifier":
		return tChar
	case "varchar": // varchar(max) too
		return tVarchar
	case "text":
		return tLongVarchar
	case "nchar":
		return tNChar
	case "nvarchar": // nvarchar(max) too
		return tNVarchar
	case "ntext", "xml":
		return tLongNVarchar
	case "binary", "timestamp":
		return tBinary
	case "varbinary": // varbinary(max) too
		return tVarbinary
	case "image":
		return tLongVarbinary
	case "date":
		return tDate
	case "time":
		return tTime
	case "datetime", "datetime2", "smalldatetime":
		return tTimestamp
	case "datetimeoffset":
		return tDateTimeOffset
	case "sql_variant":
		return tSQLVariant
	}
	return tOther
}
