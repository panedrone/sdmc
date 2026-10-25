// Package mysql is meta.Metadata on top of MySQL 8 (go-mysql-org/go-mysql: pure Go and the
// protocol fields in full: flags, charset, length, which database/sql does not expose).
//
// It mirrors Connector/J 8.4 with default settings (client-side prepare):
//   - result columns are found out by running the query with NULL instead of '?' and a limit of one
//     row; non-SELECT statements (INSERT, UPDATE, ...) have no metadata;
//   - CALL is executed too (Connector/J does so), here in a transaction that is rolled back;
//   - parameter types are java.lang.Object, except for CALL: a procedure declares them;
//   - tables, keys and indexes come from information_schema of the current database.
package mysql

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-mysql-org/go-mysql/client"
	gm "github.com/go-mysql-org/go-mysql/mysql"

	"github.com/panedrone/sdmc/internal/meta"
)

const noMetaData = "Cannot detect Prepared Statement MetaData"

type DB struct {
	conn     *client.Conn
	mbMaxLen map[uint16]int // charset id (collation) -> bytes per character
}

var _ meta.Metadata = (*DB)(nil)

// ConnParams returns the address, database, user and password from jdbc:mysql://host[:port]/db or mysql://...
func ConnParams(jdbcURL, user, pwd string) (addr, db, u, p string, err error) {
	raw := strings.TrimSpace(jdbcURL)
	raw = strings.TrimPrefix(raw, "jdbc:")
	if !strings.HasPrefix(raw, "mysql://") {
		return "", "", "", "", errors.New("MySQL URL expected (jdbc:mysql://host/db or mysql://host/db): " + jdbcURL)
	}
	pu, err := url.Parse(raw)
	if err != nil {
		return "", "", "", "", err
	}
	addr = pu.Host
	if addr == "" {
		addr = "localhost"
	}
	if pu.Port() == "" {
		addr += ":3306"
	}
	u, p = user, pwd
	if pu.User != nil { // credentials in the URL take precedence
		u = pu.User.Username()
		if s, ok := pu.User.Password(); ok {
			p = s
		}
	}
	return addr, strings.Trim(pu.Path, "/"), u, p, nil
}

func Open(addr, db, user, pwd string) (*DB, error) {
	// CALL returns results only if the client declared MULTI_RESULTS
	multiResults := func(c *client.Conn) error {
		return c.SetCapability(gm.CLIENT_MULTI_RESULTS)
	}
	conn, err := client.Connect(addr, user, pwd, db, multiResults)
	if err != nil {
		return nil, err
	}
	// result field lengths depend on the connection charset: utf8mb4, as with Connector/J
	if err := conn.SetCharset("utf8mb4"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	res := &DB{conn: conn, mbMaxLen: map[uint16]int{}}
	r, err := conn.Execute(`select c.ID, s.MAXLEN from information_schema.COLLATIONS c
		join information_schema.CHARACTER_SETS s on s.CHARACTER_SET_NAME = c.CHARACTER_SET_NAME`)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	for i := 0; i < r.RowNumber(); i++ {
		id, _ := r.GetInt(i, 0)
		ml, _ := r.GetInt(i, 1)
		res.mbMaxLen[uint16(id)] = int(ml)
	}
	return res, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// Exec is for setting up the schema in dumps.
func (db *DB) Exec(sql string) error {
	_, err := db.conn.Execute(sql)
	return err
}

func (db *DB) maxLen(charset uint16) int {
	if charset == binaryCharset {
		return 1
	}
	return db.mbMaxLen[charset]
}

// serverMessage is the server error text without the code, as Connector/J gives it.
func serverMessage(err error) string {
	var e *gm.MyError
	if errors.As(err, &e) {
		return e.Message
	}
	return err.Error()
}

// SQL statement ---------------------------------------------------

var resultSetQueries = map[string]bool{
	"select": true, "show": true, "with": true, "values": true, "table": true,
	"explain": true, "desc": true, "describe": true,
}

func (db *DB) SQLColumns(jdbcSQL string) ([]meta.Column, error) {
	sql := unescapeCall(jdbcSQL)
	word := firstWord(sql)
	if word != "call" && !resultSetQueries[word] {
		return nil, errors.New(noMetaData)
	}
	native, _ := nullParams(sql)
	fields, err := db.resultFields(native, word == "call")
	if err != nil {
		return nil, errors.New(noMetaData + ": " + serverMessage(err))
	}
	if len(fields) == 0 {
		return nil, errors.New(noMetaData)
	}
	res := make([]meta.Column, 0, len(fields))
	for i, f := range fields {
		name := string(f.Name)
		if name == "" {
			name = fmt.Sprintf("col_%d", i+1)
		}
		ff := field{typ: f.Type, flags: f.Flag, length: f.ColumnLength, decimals: f.Decimal, charset: f.Charset}
		p := ff.precision(db.maxLen)
		res = append(res, meta.Column{
			Name:        name,
			JavaType:    ff.javaType(),
			RawType:     ff.rawName(),
			IsAI:        f.Flag&flagAutoIncrement != 0,
			Scale:       ff.scale(),
			Precision:   p,
			DisplaySize: p,
		})
	}
	return res, nil
}

// resultFields returns the fields of the first result: at most one row, CALL is rolled back.
func (db *DB) resultFields(sql string, call bool) ([]*gm.Field, error) {
	if _, err := db.conn.Execute("SET SQL_SELECT_LIMIT = 1"); err != nil {
		return nil, err
	}
	defer func() { _, _ = db.conn.Execute("SET SQL_SELECT_LIMIT = DEFAULT") }()
	if !call {
		r, err := db.conn.Execute(sql)
		if err != nil {
			return nil, err
		}
		if r.Resultset == nil {
			return nil, nil
		}
		return r.Fields, nil
	}
	if err := db.conn.Begin(); err != nil {
		return nil, err
	}
	defer func() { _ = db.conn.Rollback() }()
	var fields []*gm.Field
	var callErr error
	_, err := db.conn.ExecuteMultiple(sql, func(r *gm.Result, err error) {
		switch {
		case err != nil && callErr == nil:
			callErr = err
		case err == nil && fields == nil && r != nil && r.Resultset != nil && len(r.Fields) > 0:
			fields = r.Fields
		}
	})
	if callErr != nil {
		return nil, callErr
	}
	return fields, err
}

func (db *DB) SQLParams(jdbcSQL string, typeCount int) (*meta.Params, error) {
	sql := unescapeCall(jdbcSQL)
	_, n := nullParams(sql)
	res := &meta.Params{Count: n, JavaTypes: make([]string, typeCount), RawTypes: make([]string, typeCount)}
	for i := range res.JavaTypes {
		res.JavaTypes[i] = meta.JavaObject
	}
	proc, args := callArgs(sql)
	if proc == "" {
		return res, nil
	}
	declared, raws, err := db.procParams(proc)
	if err != nil {
		return nil, err
	}
	// the types of '?' come from their position among the procedure arguments
	k := 0
	for i, a := range args {
		if a != "?" {
			continue
		}
		if k < typeCount && i < len(declared) {
			res.JavaTypes[k] = declared[i]
			res.RawTypes[k] = raws[i]
		}
		k++
	}
	return res, nil
}

func (db *DB) procParams(proc string) (javaTypes, rawTypes []string, err error) {
	proc = strings.ReplaceAll(proc, "`", "")
	schema := "DATABASE()"
	args := []any{}
	if i := strings.LastIndexByte(proc, '.'); i >= 0 {
		schema = "?"
		args = append(args, proc[:i])
		proc = proc[i+1:]
	}
	args = append(args, proc)
	r, err := db.conn.Execute(`select DATA_TYPE, DTD_IDENTIFIER from information_schema.PARAMETERS
		where SPECIFIC_SCHEMA = `+schema+` and SPECIFIC_NAME = ? and ROUTINE_TYPE = 'PROCEDURE'
		  and ORDINAL_POSITION > 0 order by ORDINAL_POSITION`, args...)
	if err != nil {
		return nil, nil, err
	}
	for i := 0; i < r.RowNumber(); i++ {
		dt, _ := r.GetString(i, 0)
		dtd, _ := r.GetString(i, 1)
		dt, dtd = strings.ToLower(dt), strings.ToLower(dtd)
		javaTypes = append(javaTypes, paramJavaType(dt, dtd))
		rawTypes = append(rawTypes, paramRawName(dt, dtd))
	}
	return javaTypes, rawTypes, nil
}

// Table -----------------------------------------------------------

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

// schemaExpr is the current database or the one given explicitly.
func schemaExpr(schema string, args *[]any) string {
	if schema == "" {
		return "DATABASE()"
	}
	*args = append(*args, schema)
	return "?"
}

func (db *DB) Table(tableRef string) (*meta.Table, error) {
	schema, table, err := splitSchema(tableRef)
	if err != nil {
		return nil, err
	}
	args := []any{}
	s := schemaExpr(schema, &args)
	r, err := db.conn.Execute(`select count(*) from information_schema.TABLES
		where TABLE_SCHEMA = `+s+` and TABLE_NAME like ? and TABLE_TYPE in ('BASE TABLE', 'VIEW')`,
		append(args, table)...)
	if err != nil {
		return nil, err
	}
	if n, _ := r.GetInt(0, 0); n == 0 {
		return nil, errors.New("Not found: '" + tableRef + "'. The search is case-sensitive.")
	}
	columns, err := db.SQLColumns("select * from " + tableRef)
	if err != nil {
		return nil, err
	}
	tableColumns, err := db.tableColumns(tableRef)
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
	unique, err := db.indexedColumns(tableRef, true)
	if err != nil {
		return nil, err
	}
	indexed, err := db.indexedColumns(tableRef, false)
	if err != nil {
		return nil, err
	}
	fk, err := db.importedKeys(tableRef)
	if err != nil {
		return nil, err
	}
	args := []any{}
	s := schemaExpr(schema, &args)
	r, err := db.conn.Execute(`select COLUMN_NAME, lower(DATA_TYPE), lower(COLUMN_TYPE), CHARACTER_MAXIMUM_LENGTH,
			NUMERIC_PRECISION, NUMERIC_SCALE, DATETIME_PRECISION, IS_NULLABLE, EXTRA
		from information_schema.COLUMNS
		where TABLE_SCHEMA = `+s+` and TABLE_NAME like ?
		order by TABLE_NAME, ORDINAL_POSITION`, append(args, table)...)
	if err != nil {
		return nil, err
	}
	res := make([]meta.TableColumn, 0, r.RowNumber())
	for i := 0; i < r.RowNumber(); i++ {
		name, _ := r.GetString(i, 0)
		var c column
		c.dataType, _ = r.GetString(i, 1)
		c.columnType, _ = r.GetString(i, 2)
		c.charLen = nullableInt(r, i, 3)
		c.numPrec = nullableInt(r, i, 4)
		c.numScale = nullableInt(r, i, 5)
		c.dtPrec = nullableInt(r, i, 6)
		isNullable, _ := r.GetString(i, 7)
		extra, _ := r.GetString(i, 8)
		dataType, size, digits := c.sqlType(), c.columnSize(), c.decimalDigits()
		ai := strings.Contains(strings.ToLower(extra), "auto_increment")
		nullable := isNullable == "YES"
		lo := strings.ToLower(name)
		_, isIndexed := indexed[lo]
		_, isUnique := unique[lo]
		res = append(res, meta.TableColumn{
			Name:          name,
			DataType:      &dataType,
			ColumnSize:    &size,
			DecimalDigits: &digits,
			IsAI:          &ai,
			IsNullable:    &nullable,
			FK:            fk[lo],
			IsIndexed:     isIndexed,
			IsUnique:      isUnique,
		})
	}
	return res, nil
}

func nullableInt(r *gm.Result, row, col int) *int64 {
	if null, _ := r.IsNull(row, col); null {
		return nil
	}
	v, err := r.GetInt(row, col)
	if err != nil {
		return nil
	}
	return &v
}

func (db *DB) indexedColumns(table string, uniqueOnly bool) (map[string]struct{}, error) {
	q := `select COLUMN_NAME from information_schema.STATISTICS
		where TABLE_SCHEMA = DATABASE() and TABLE_NAME = ? and COLUMN_NAME is not null`
	if uniqueOnly {
		q += ` and NON_UNIQUE = 0`
	}
	r, err := db.conn.Execute(q, table)
	if err != nil {
		return nil, err
	}
	res := map[string]struct{}{}
	for i := 0; i < r.RowNumber(); i++ {
		s, _ := r.GetString(i, 0)
		res[strings.ToLower(s)] = struct{}{}
	}
	return res, nil
}

func (db *DB) importedKeys(table string) (map[string]string, error) {
	r, err := db.conn.Execute(`select COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		from information_schema.KEY_COLUMN_USAGE
		where TABLE_SCHEMA = DATABASE() and TABLE_NAME = ? and REFERENCED_TABLE_NAME is not null
		order by REFERENCED_TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION`, table)
	if err != nil {
		return nil, err
	}
	res := map[string]string{}
	for i := 0; i < r.RowNumber(); i++ {
		c, _ := r.GetString(i, 0)
		t, _ := r.GetString(i, 1)
		rc, _ := r.GetString(i, 2)
		res[strings.ToLower(c)] = t + "." + rc
	}
	return res, nil
}

func (db *DB) PKColNames(tableRef string) ([]string, error) {
	schema, table, err := splitSchema(tableRef)
	if err != nil {
		return nil, err
	}
	args := []any{}
	s := schemaExpr(schema, &args)
	r, err := db.conn.Execute(`select COLUMN_NAME from information_schema.STATISTICS
		where TABLE_SCHEMA = `+s+` and TABLE_NAME = ? and INDEX_NAME = 'PRIMARY'
		order by SEQ_IN_INDEX`, append(args, table)...)
	if err != nil {
		return nil, err
	}
	res := []string{}
	for i := 0; i < r.RowNumber(); i++ {
		c, _ := r.GetString(i, 0)
		res = append(res, c)
	}
	return res, nil
}
