// Package meta is what the database says about SQL queries and tables, as is: no settings,
// no type-map, no DTO fields. It mirrors IDbMetadata of the Java plug-in one to one, so that
// the generator output can be compared byte for byte (cli/metadump against MetaDump.java).
//
// Types are named by Java names (java.lang.Integer, java.util.Date, ...): the keys
// <type detected=...> in settings.xml of existing projects depend on them.
//
// RawType is the same in the driver's words: the second vocabulary of type-map keys, on a par with Java names.
package meta

// Metadata has one implementation per dialect (pg, mysql, sqlite, mssql).
type Metadata interface {
	// SQLColumns returns the result columns of jdbcSQL (SQL with '?').
	SQLColumns(jdbcSQL string) ([]Column, error)

	// Table runs 'select * from tableRef' and adds what the catalog says about the columns.
	Table(tableRef string) (*Table, error)

	// PKColNames returns the names of the PK columns in key order.
	PKColNames(tableRef string) ([]string, error)

	// SQLParams returns the parameters of jdbcSQL; types are read for indexes 0..typeCount-1.
	SQLParams(jdbcSQL string, typeCount int) (*Params, error)

	// RawOfJava returns a Java type name from the plug-in XML (java.lang.Integer) in the words of this database's driver
	// (pg: int4, MySQL: INT, SQLite: INTEGER); "" means unknown.
	RawOfJava(javaType string) string

	Close() error
}

// Catalog is what the XML assistants read from the database (SdmUtils of the Java plug-in):
// getTables and getImportedKeys.
type Catalog interface {
	// Tables lists the tables and, if includeViews, the views of the database, as
	// getTables(null, "%", TABLE, VIEW) sorts them: tables first, then views, by name.
	Tables(includeViews bool) ([]string, error)
	// ImportedKeys lists the foreign key columns of table: by PK table, then key order.
	ImportedKeys(table string) ([]ImportedKey, error)
	// IdentifierQuote is getIdentifierQuoteString: the quote of the names the assistants write
	// quoted when they are not plain names (QuoteName).
	IdentifierQuote() string
}

// ImportedKey is a row of getImportedKeys: PKTABLE_NAME and FKCOLUMN_NAME.
type ImportedKey struct {
	PKTable  string
	FKColumn string
}

const (
	JavaObject = "java.lang.Object"
	// RawUnknown is the raw name of a type the driver said nothing about (a type-map key)
	RawUnknown = "unknown"
)

// Column is a result column (ResultSetMetaData).
type Column struct {
	Name        string
	JavaType    string // JavaObject if not determined
	RawType     string // the type in the driver's words (pg: int4, MySQL: INT, SQLite: INTEGER); "" means not reported
	IsAI        bool
	Scale       int
	Precision   int
	DisplaySize int
}

// TableColumn is a row of DatabaseMetaData.getColumns. nil means the driver could not report it.
type TableColumn struct {
	Name          string
	DataType      *int   // java.sql.Types
	RawType       string // the type in the driver's words; needed where the result column does not know it (SQLite)
	ColumnSize    *int
	DecimalDigits *int
	IsAI          *bool
	IsNullable    *bool
	FK            string // "pk_table.pk_column" or ""
	IsIndexed     bool
	IsUnique      bool
}

type Table struct {
	Columns []Column // 'select * from table'
	// in getColumns order; the same column may come several times (tables of the same
	// name in different schemas), and then each row refines it once more
	TableColumns []TableColumn
}

type Params struct {
	CountUnknown bool // parameter metadata exists, but their number could not be found out
	Count        int
	JavaTypes    []string // JavaObject where not determined
	RawTypes     []string // types in the driver's words; "" means not reported (same length as JavaTypes)
}
