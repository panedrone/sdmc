// Package mssql is meta.Metadata on top of microsoft/go-mssqldb. Queries are not executed: result
// columns come from sp_describe_first_result_set and parameters from sp_describe_undeclared_parameters
// (procedures: their parameters), the rest from the sys catalog by the rules of mssql-jdbc
// DatabaseMetaData, to match the Java plug-in.
package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	mssqldb "github.com/microsoft/go-mssqldb"

	"github.com/panedrone/sdmc/internal/meta"
)

const noMetaData = "Cannot detect Prepared Statement MetaData"

type DB struct {
	ctx context.Context
	db  *sql.DB
}

var _ meta.Metadata = (*DB)(nil)

// Open connects by a go-mssqldb connection string: sqlserver://user:password@host:1433?database=name.
func Open(connString string) (*DB, error) {
	db, err := sql.Open("sqlserver", connString)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{ctx: ctx, db: db}, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

// Exec is for setting up the schema in dumps.
func (db *DB) Exec(q string) error {
	_, err := db.db.ExecContext(db.ctx, q)
	return err
}

// serverMessage is the server error text without the number, as mssql-jdbc gives it: the first of the
// errors of the batch, the cause ("Cannot find either column..."), not the summary after it.
func serverMessage(err error) string {
	var e mssqldb.Error
	if errors.As(err, &e) {
		if len(e.All) > 0 {
			return e.All[0].Message
		}
		return e.Message
	}
	return err.Error()
}

// rowMaps reads all rows as column name -> value: the result sets of the system procedures have many columns.
func rowMaps(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var res []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = vals[i]
		}
		res = append(res, m)
	}
	return res, rows.Err()
}

func toInt(v any) int {
	switch x := v.(type) {
	case int64:
		return int(x)
	case int32:
		return int(x)
	case int16:
		return int(x)
	case uint8:
		return int(x)
	case int:
		return x
	case []byte:
		n, _ := strconv.Atoi(string(x))
		return n
	}
	return 0
}

func toBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	}
	return false
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	}
	return ""
}

// baseType is the system type of a type name like "nvarchar(15)" or "decimal(10,2)".
func baseType(name string) string {
	if i := strings.IndexByte(name, '('); i >= 0 {
		name = name[:i]
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// declaration is the type for @params of sp_describe_first_result_set: nvarchar(15), decimal(10,2), ...
func (t colType) declaration() string {
	switch t.name {
	case "char", "varchar", "nchar", "nvarchar", "binary", "varbinary":
		if t.maxLength < 0 {
			return t.name + "(max)"
		}
		return t.name + "(" + strconv.Itoa(t.length()) + ")"
	case "decimal", "numeric":
		return fmt.Sprintf("%s(%d,%d)", t.name, t.precision, t.scale)
	case "time", "datetime2", "datetimeoffset":
		return fmt.Sprintf("%s(%d)", t.name, t.scale)
	}
	return t.name
}

// SQL statement ---------------------------------------------------

func (db *DB) SQLColumns(jdbcSQL string) ([]meta.Column, error) {
	native, n, proc := toNative(jdbcSQL)
	var params any // NULL: no parameters
	if n > 0 {
		types, err := db.paramTypes(native, n, proc)
		if err != nil {
			types = nil
		}
		decl := make([]string, n)
		for i := range decl {
			t := "nvarchar(4000)" // what the server could not find out
			if i < len(types) && types[i].name != "" {
				t = types[i].declaration()
			}
			decl[i] = "@P" + strconv.Itoa(i+1) + " " + t
		}
		params = strings.Join(decl, ", ")
	}
	rows, err := db.db.QueryContext(db.ctx, `exec sp_describe_first_result_set @tsql = @p1, @params = @p2, @browse_information_mode = 0`,
		native, params)
	if err != nil {
		return nil, errors.New(noMetaData + ": " + serverMessage(err))
	}
	ms, err := rowMaps(rows)
	if err != nil {
		return nil, errors.New(noMetaData + ": " + serverMessage(err))
	}
	var res []meta.Column
	for _, m := range ms {
		if toBool(m["is_hidden"]) {
			continue
		}
		name := toString(m["name"])
		if name == "" {
			name = "col_" + strconv.Itoa(len(res)+1) // count(*), dbo.ufnLeadingZeros(?), ...
		}
		t := colType{name: baseType(toString(m["system_type_name"])), maxLength: toInt(m["max_length"]),
			precision: toInt(m["precision"]), scale: toInt(m["scale"])}
		res = append(res, meta.Column{
			Name:        name,
			JavaType:    t.javaType(),
			RawType:     t.name,
			IsAI:        toBool(m["is_identity_column"]),
			Scale:       t.rsScale(),
			Precision:   t.rsPrecision(),
			DisplaySize: t.displaySize(),
		})
	}
	if len(res) == 0 {
		return nil, errors.New(noMetaData) // INSERT, UPDATE, ...: no result set
	}
	return res, nil
}

// paramTypes are the types of @P1..@Pn: of the procedure parameters for a call, suggested by the server else.
func (db *DB) paramTypes(native string, n int, proc string) ([]colType, error) {
	res := make([]colType, n)
	if proc != "" {
		ptypes, err := db.procParams(proc)
		if err != nil {
			return nil, err
		}
		// the position of @Pk among the arguments of EXEC is the position of the procedure parameter
		args := splitArgs(strings.TrimSpace(strings.TrimPrefix(native, "EXEC "+proc)))
		for i, a := range args {
			if k, ok := strings.CutPrefix(a, "@P"); ok {
				if idx, err := strconv.Atoi(k); err == nil && idx >= 1 && idx <= n && i < len(ptypes) {
					res[idx-1] = ptypes[i]
				}
			}
		}
		return res, nil
	}
	rows, err := db.db.QueryContext(db.ctx, `exec sp_describe_undeclared_parameters @tsql = @p1`, native)
	if err != nil {
		return nil, err
	}
	ms, err := rowMaps(rows)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		k, ok := strings.CutPrefix(toString(m["name"]), "@P")
		if !ok {
			continue
		}
		idx, err := strconv.Atoi(k)
		if err != nil || idx < 1 || idx > n {
			continue
		}
		res[idx-1] = colType{name: baseType(toString(m["suggested_system_type_name"])),
			maxLength: toInt(m["suggested_max_length"]), precision: toInt(m["suggested_precision"]),
			scale: toInt(m["suggested_scale"])}
	}
	return res, nil
}

// splitArgs splits the arguments of EXEC by the commas outside of strings and quoted names.
func splitArgs(s string) []string {
	if s == "" {
		return nil
	}
	var res []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			i = skipQuoted(s, i, '\'') - 1
		case '[':
			i = skipQuoted(s, i, ']') - 1
		case '"':
			i = skipQuoted(s, i, '"') - 1
		case ',':
			res = append(res, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(res, strings.TrimSpace(s[start:]))
}

func (db *DB) procParams(proc string) ([]colType, error) {
	rows, err := db.db.QueryContext(db.ctx, `select coalesce(type_name(p.system_type_id), type_name(p.user_type_id)), p.max_length, p.precision, p.scale
		from sys.parameters p
		where p.object_id = object_id(@p1) and p.parameter_id > 0
		order by p.parameter_id`, proc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []colType
	for rows.Next() {
		var t colType
		if err := rows.Scan(&t.name, &t.maxLength, &t.precision, &t.scale); err != nil {
			return nil, err
		}
		t.name = strings.ToLower(t.name)
		res = append(res, t)
	}
	return res, rows.Err()
}

func (db *DB) SQLParams(jdbcSQL string, typeCount int) (*meta.Params, error) {
	native, n, proc := toNative(jdbcSQL)
	res := &meta.Params{Count: n, JavaTypes: make([]string, typeCount), RawTypes: make([]string, typeCount)}
	for i := range res.JavaTypes {
		res.JavaTypes[i] = meta.JavaObject
	}
	if n == 0 {
		return res, nil
	}
	types, err := db.paramTypes(native, n, proc)
	if err == nil && slices.ContainsFunc(types, func(t colType) bool { return t.name == "hierarchyid" }) {
		err = errors.New("hierarchyid") // mssql-jdbc cannot describe a hierarchyid parameter
	}
	if err != nil {
		res.Count = 0 // getParameterMetaData throws: the plug-in takes 0 parameters
		return res, nil
	}
	for i := 0; i < typeCount && i < len(types); i++ {
		if types[i].name != "" {
			res.JavaTypes[i] = types[i].javaType()
			if types[i].clr() { // geography, geometry: mssql-jdbc gives no class name for the parameter
				res.JavaTypes[i] = meta.JavaObject
			}
			res.RawTypes[i] = types[i].name
		}
	}
	return res, nil
}

// Table -----------------------------------------------------------

// splitSchema splits "schema.table" of exactly two parts, as getTables/getPrimaryKeys in Java.
func splitSchema(tableRef string) (schema, table string, err error) {
	if !strings.Contains(tableRef, ".") {
		return "", tableRef, nil
	}
	parts := strings.Split(tableRef, ".")
	if len(parts) != 2 {
		return "", "", errors.New("Invalid table name: '" + tableRef + "'")
	}
	return parts[0], parts[1], nil
}

// nullable is a schema for SQL: NULL means any.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (db *DB) Table(tableRef string) (*meta.Table, error) {
	// SQL takes the name as it is written ([Order Details]), the catalog without the quotes
	mdRef := meta.UnquoteTableRef(tableRef)
	schema, table, err := splitSchema(mdRef)
	if err != nil {
		return nil, err
	}
	var n int
	err = db.db.QueryRowContext(db.ctx, `select count(*) from sys.objects o
		join sys.schemas s on s.schema_id = o.schema_id
		where o.type in ('U', 'V') and o.name like @p1 and (@p2 is null or s.name like @p2)`,
		table, nullable(schema)).Scan(&n)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("Not found: '" + mdRef + "'. The search is case-sensitive.")
	}
	columns, err := db.SQLColumns("select * from " + tableRef)
	if err != nil {
		return nil, err
	}
	tableColumns, err := db.tableColumns(mdRef)
	if err != nil {
		return nil, err
	}
	return &meta.Table{Columns: columns, TableColumns: tableColumns}, nil
}

func (db *DB) tableColumns(tableRef string) ([]meta.TableColumn, error) {
	schema, table := "", tableRef
	if i := strings.LastIndexByte(tableRef, '.'); i >= 0 {
		schema, table = tableRef[:i], tableRef[i+1:]
	}
	// Java passes the full name (with the schema) to getIndexInfo/getImportedKeys; repeated here
	unique, err := db.indexedColumns(schema, tableRef, true)
	if err != nil {
		return nil, err
	}
	indexed, err := db.indexedColumns(schema, tableRef, false)
	if err != nil {
		return nil, err
	}
	fk, err := db.importedKeys(schema, tableRef)
	if err != nil {
		return nil, err
	}
	rows, err := db.db.QueryContext(db.ctx, `select c.name, coalesce(type_name(c.system_type_id), type_name(c.user_type_id)), c.max_length, c.precision, c.scale,
			c.is_identity, c.is_nullable
		from sys.columns c
		join sys.objects o on o.object_id = c.object_id
		join sys.schemas s on s.schema_id = o.schema_id
		where o.type in ('U', 'V') and o.name like @p1 and (@p2 is null or s.name like @p2)
		order by s.name, o.name, c.column_id`, table, nullable(schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []meta.TableColumn
	for rows.Next() {
		var name string
		var t colType
		var ai, isNullable bool
		if err := rows.Scan(&name, &t.name, &t.maxLength, &t.precision, &t.scale, &ai, &isNullable); err != nil {
			return nil, err
		}
		t.name = strings.ToLower(t.name)
		dataType, size, digits := t.sqlType(), t.columnSize(), t.decimalDigits()
		lo := strings.ToLower(name)
		_, isIndexed := indexed[lo]
		_, isUnique := unique[lo]
		res = append(res, meta.TableColumn{
			Name:          name,
			DataType:      &dataType,
			RawType:       t.name,
			ColumnSize:    &size,
			DecimalDigits: &digits,
			IsAI:          &ai,
			IsNullable:    &isNullable,
			FK:            fk[lo],
			IsIndexed:     isIndexed,
			IsUnique:      isUnique,
		})
	}
	return res, rows.Err()
}

// indexedColumns: the key columns of the indexes of table, as getIndexInfo (sp_statistics) gives them.
func (db *DB) indexedColumns(schema, table string, uniqueOnly bool) (map[string]struct{}, error) {
	q := `select c.name from sys.indexes i
		join sys.index_columns ic on ic.object_id = i.object_id and ic.index_id = i.index_id
		join sys.columns c on c.object_id = ic.object_id and c.column_id = ic.column_id
		join sys.objects o on o.object_id = i.object_id
		join sys.schemas s on s.schema_id = o.schema_id
		where i.type > 0 and ic.is_included_column = 0 and o.name = @p1 and (@p2 is null or s.name = @p2)`
	if uniqueOnly {
		q += ` and i.is_unique = 1`
	}
	rows, err := db.db.QueryContext(db.ctx, q, table, nullable(schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]struct{}{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		res[strings.ToLower(s)] = struct{}{}
	}
	return res, rows.Err()
}

// importedKeys: FK column (lower case) -> "pk_table.pk_column", in the order of getImportedKeys.
func (db *DB) importedKeys(schema, table string) (map[string]string, error) {
	rows, err := db.db.QueryContext(db.ctx, `select fc.name, pt.name, pc.name
		from sys.foreign_key_columns k
		join sys.objects ft on ft.object_id = k.parent_object_id
		join sys.schemas fs on fs.schema_id = ft.schema_id
		join sys.objects pt on pt.object_id = k.referenced_object_id
		join sys.schemas ps on ps.schema_id = pt.schema_id
		join sys.columns fc on fc.object_id = k.parent_object_id and fc.column_id = k.parent_column_id
		join sys.columns pc on pc.object_id = k.referenced_object_id and pc.column_id = k.referenced_column_id
		where ft.name = @p1 and (@p2 is null or fs.name = @p2)
		order by ps.name, pt.name, k.constraint_column_id`, table, nullable(schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]string{}
	for rows.Next() {
		var c, t, pc string
		if err := rows.Scan(&c, &t, &pc); err != nil {
			return nil, err
		}
		res[strings.ToLower(c)] = t + "." + pc
	}
	return res, rows.Err()
}

func (db *DB) PKColNames(tableRef string) ([]string, error) {
	schema, table, err := splitSchema(meta.UnquoteTableRef(tableRef))
	if err != nil {
		return nil, err
	}
	rows, err := db.db.QueryContext(db.ctx, `select c.name from sys.indexes i
		join sys.index_columns ic on ic.object_id = i.object_id and ic.index_id = i.index_id
		join sys.columns c on c.object_id = ic.object_id and c.column_id = ic.column_id
		join sys.objects o on o.object_id = i.object_id
		join sys.schemas s on s.schema_id = o.schema_id
		where i.is_primary_key = 1 and o.name = @p1 and (@p2 is null or s.name = @p2)
		order by ic.key_ordinal`, table, nullable(schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		res = append(res, c)
	}
	return res, rows.Err()
}
