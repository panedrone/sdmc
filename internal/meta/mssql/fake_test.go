package mssql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	mssqldb "github.com/microsoft/go-mssqldb"

	"github.com/panedrone/sdmc/internal/meta"
)

// A database/sql driver that answers the catalog queries of this package with rows as SQL Server
// gives them for Northwind: the SQL of the queries and the reading of the rows run without a server.

type fakeResult struct {
	cols []string
	rows [][]driver.Value
	err  error
}

// fakeAnswer picks the answer by the query text and its arguments.
func fakeAnswer(q string, args []driver.NamedValue) fakeResult {
	arg := func(i int) any {
		if i < len(args) {
			return args[i].Value
		}
		return nil
	}
	switch {
	case strings.Contains(q, "sp_describe_first_result_set"):
		tsql, _ := arg(0).(string)
		cols := []string{"is_hidden", "name", "system_type_name", "max_length", "precision", "scale", "is_identity_column"}
		switch {
		case strings.Contains(tsql, "Bad"):
			return fakeResult{err: mssqldb.Error{Message: "summary", All: []mssqldb.Error{{Message: "Invalid object name 'Bad'."}}}}
		case strings.HasPrefix(tsql, "update"):
			return fakeResult{cols: cols}
		case strings.Contains(tsql, "count(*)"):
			return fakeResult{cols: cols, rows: [][]driver.Value{{false, "", "int", int64(4), int64(10), int64(0), false}}}
		}
		return fakeResult{cols: cols, rows: [][]driver.Value{
			{false, "OrderID", "int", int64(4), int64(10), int64(0), true},
			{false, "CustomerID", "nchar(5)", int64(10), int64(0), int64(0), false},
			{false, "Freight", "money", int64(8), int64(19), int64(4), false},
			{true, "hidden_key", "int", int64(4), int64(10), int64(0), false},
		}}
	case strings.Contains(q, "sp_describe_undeclared_parameters"):
		return fakeResult{cols: []string{"name", "suggested_system_type_name", "suggested_max_length", "suggested_precision", "suggested_scale"},
			rows: [][]driver.Value{
				{"@P1", "int", int64(4), int64(10), int64(0)},
				{"@P2", "nvarchar(30)", int64(60), int64(0), int64(0)},
				{"@X", "int", int64(4), int64(10), int64(0)},  // not a parameter of the statement
				{"@P9", "int", int64(4), int64(10), int64(0)}, // out of range
			}}
	case strings.Contains(q, "from sys.parameters"):
		if arg(0) == "HierProc" {
			return fakeResult{cols: []string{"t", "l", "p", "s"}, rows: [][]driver.Value{{"hierarchyid", int64(892), int64(0), int64(0)}}}
		}
		return fakeResult{cols: []string{"t", "l", "p", "s"}, rows: [][]driver.Value{
			{"NCHAR", int64(10), int64(0), int64(0)},
			{"datetime", int64(8), int64(23), int64(3)},
		}}
	case strings.Contains(q, "select count(*) from sys.objects"):
		n := int64(1)
		if arg(0) == "Nope" {
			n = 0
		}
		return fakeResult{cols: []string{"n"}, rows: [][]driver.Value{{n}}}
	case strings.Contains(q, "from sys.columns c"):
		return fakeResult{cols: []string{"name", "type", "max_length", "precision", "scale", "is_identity", "is_nullable"},
			rows: [][]driver.Value{
				{"OrderID", "int", int64(4), int64(10), int64(0), true, false},
				{"CustomerID", "nchar", int64(10), int64(0), int64(0), false, true},
				{"Freight", "money", int64(8), int64(19), int64(4), false, true},
			}}
	case strings.Contains(q, "is_primary_key"):
		return fakeResult{cols: []string{"name"}, rows: [][]driver.Value{{"OrderID"}}}
	case strings.Contains(q, "from sys.indexes"):
		if strings.Contains(q, "is_unique") {
			return fakeResult{cols: []string{"name"}, rows: [][]driver.Value{{"OrderID"}}}
		}
		return fakeResult{cols: []string{"name"}, rows: [][]driver.Value{{"OrderID"}, {"CustomerID"}}}
	case strings.Contains(q, "fs.schema_id"):
		return fakeResult{cols: []string{"c", "t", "pc"}, rows: [][]driver.Value{{"CustomerID", "Customers", "CustomerID"}}}
	case strings.Contains(q, "foreign_key_columns"):
		return fakeResult{cols: []string{"pt", "fc"}, rows: [][]driver.Value{{"Customers", "CustomerID"}, {"Employees", "EmployeeID"}}}
	case strings.Contains(q, "is_ms_shipped"):
		if arg(0) == true {
			return fakeResult{cols: []string{"name"}, rows: [][]driver.Value{{"Orders"}, {"Invoices"}}}
		}
		return fakeResult{cols: []string{"name"}, rows: [][]driver.Value{{"Orders"}}}
	}
	return fakeResult{err: errors.New("fake: unexpected query: " + q)}
}

type fakeDriver struct{}
type fakeConn struct{}
type fakeRows struct {
	r fakeResult
	i int
}

func (fakeDriver) Open(string) (driver.Conn, error) { return fakeConn{}, nil }

func (fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("fake: no Prepare") }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("fake: no Begin") }

func (fakeConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	r := fakeAnswer(q, args)
	if r.err != nil {
		return nil, r.err
	}
	return &fakeRows{r: r}, nil
}

func (fakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}

func (r *fakeRows) Columns() []string { return r.r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= len(r.r.rows) {
		return io.EOF
	}
	copy(dest, r.r.rows[r.i])
	r.i++
	return nil
}

func init() { sql.Register("fake-mssql", fakeDriver{}) }

func fakeDB(t *testing.T) *DB {
	t.Helper()
	db, err := sql.Open("fake-mssql", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &DB{ctx: context.Background(), db: db}
}

func TestFakeSQLColumns(t *testing.T) {
	db := fakeDB(t)
	cols, err := db.SQLColumns("select * from Orders where CustomerID = ? and ShipName = ?")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 { // the hidden column is left out
		t.Fatalf("%+v", cols)
	}
	if c := cols[0]; c.Name != "OrderID" || c.JavaType != "java.lang.Integer" || !c.IsAI {
		t.Errorf("OrderID: %+v", c)
	}
	if c := cols[1]; c.RawType != "nchar" || c.Precision != 5 {
		t.Errorf("CustomerID: %+v", c)
	}
	if c := cols[2]; c.JavaType != "java.math.BigDecimal" || c.Scale != 4 {
		t.Errorf("Freight: %+v", c)
	}
	cols, err = db.SQLColumns("select count(*) from Orders")
	if err != nil || len(cols) != 1 || cols[0].Name != "col_1" {
		t.Errorf("count(*): %+v %v", cols, err)
	}
	if _, err := db.SQLColumns("select * from Bad"); err == nil || !strings.Contains(err.Error(), "Invalid object name 'Bad'.") {
		t.Errorf("Bad: %v", err)
	}
	if _, err := db.SQLColumns("update Orders set Freight = 0"); err == nil || err.Error() != noMetaData {
		t.Errorf("update: %v", err)
	}
	// a procedure call: the types of its parameters
	if _, err := db.SQLColumns("{call CustOrdersOrders(?, ?)}"); err != nil {
		t.Errorf("call: %v", err)
	}
}

func TestFakeSQLParams(t *testing.T) {
	db := fakeDB(t)
	p, err := db.SQLParams("select * from Orders where OrderID = ? and ShipName = ?", 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Count != 2 || !reflect.DeepEqual(p.JavaTypes, []string{"java.lang.Integer", "java.lang.String"}) ||
		!reflect.DeepEqual(p.RawTypes, []string{"int", "nvarchar"}) {
		t.Errorf("%+v", p)
	}
	p, err = db.SQLParams("{call CustOrdersOrders(?, ?)}", 2)
	if err != nil || p.Count != 2 || p.RawTypes[0] != "nchar" || p.JavaTypes[1] != "java.util.Date" {
		t.Errorf("call: %+v %v", p, err)
	}
	// mssql-jdbc cannot describe hierarchyid: 0 parameters, as the plug-in takes it
	if p, err := db.SQLParams("{call HierProc(?)}", 1); err != nil || p.Count != 0 {
		t.Errorf("hierarchyid: %+v %v", p, err)
	}
	if p, err := db.SQLParams("select 1", 0); err != nil || p.Count != 0 {
		t.Errorf("no params: %+v %v", p, err)
	}
}

func TestFakeTable(t *testing.T) {
	db := fakeDB(t)
	tb, err := db.Table("dbo.Orders")
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.Columns) != 3 || len(tb.TableColumns) != 3 {
		t.Fatalf("%+v", tb)
	}
	id, cust := tb.TableColumns[0], tb.TableColumns[1]
	if !*id.IsAI || *id.IsNullable || !id.IsIndexed || !id.IsUnique || *id.DataType != 4 {
		t.Errorf("OrderID: %+v", id)
	}
	if cust.FK != "Customers.CustomerID" || !cust.IsIndexed || cust.IsUnique || *cust.ColumnSize != 5 {
		t.Errorf("CustomerID: %+v", cust)
	}
	if _, err := db.Table("Nope"); err == nil || !strings.Contains(err.Error(), "Not found: 'Nope'") {
		t.Errorf("Nope: %v", err)
	}
	if _, err := db.Table("a.b.c"); err == nil {
		t.Error("a.b.c: no error")
	}
	pk, err := db.PKColNames("[dbo].[Orders]")
	if err != nil || !reflect.DeepEqual(pk, []string{"OrderID"}) {
		t.Errorf("PK: %v %v", pk, err)
	}
	if _, err := db.PKColNames("a.b.c"); err == nil {
		t.Error("PK a.b.c: no error")
	}
}

func TestFakeCatalog(t *testing.T) {
	db := fakeDB(t)
	tables, err := db.Tables(false)
	if err != nil || !reflect.DeepEqual(tables, []string{"Orders"}) {
		t.Errorf("tables: %v %v", tables, err)
	}
	if all, err := db.Tables(true); err != nil || len(all) != 2 {
		t.Errorf("with views: %v %v", all, err)
	}
	keys, err := db.ImportedKeys("Orders")
	if err != nil || !reflect.DeepEqual(keys, []meta.ImportedKey{{PKTable: "Customers", FKColumn: "CustomerID"}, {PKTable: "Employees", FKColumn: "EmployeeID"}}) {
		t.Errorf("keys: %v %v", keys, err)
	}
	if err := db.Exec("create table t (id int)"); err != nil {
		t.Errorf("Exec: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
