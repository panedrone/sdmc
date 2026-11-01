package sqlite

import (
	"reflect"
	"testing"

	"github.com/panedrone/sdmc/internal/meta"
)

func TestCatalog(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		"create table orders (o_id integer primary key autoincrement, c_id integer references customers(c_id))",
		"create table customers (c_id integer primary key)",
		"create table items (o_id integer, p_id integer, alt_p_id integer," +
			" foreign key (p_id) references products(p_id), foreign key (o_id) references orders(o_id)," +
			" foreign key (alt_p_id) references products(p_id))",
		"create table products (p_id integer primary key)",
		"create view a_view as select * from orders",
	} {
		if err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.Tables(true)
	if err != nil {
		t.Fatal(err)
	}
	// no sqlite_sequence; tables before views
	if want := []string{"customers", "items", "orders", "products", "a_view"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tables: %v", got)
	}
	if got, _ := db.Tables(false); len(got) != 4 {
		t.Errorf("without views: %v", got)
	}
	keys, err := db.ImportedKeys("items")
	if err != nil {
		t.Fatal(err)
	}
	want := []meta.ImportedKey{
		{PKTable: "orders", FKColumn: "o_id"},
		{PKTable: "products", FKColumn: "p_id"},
		{PKTable: "products", FKColumn: "alt_p_id"}, // in the order of declaration
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys: %v", keys)
	}
}

// A table with a space in its name: refs quote it, the catalog is asked without the quotes.
func TestQuotedTableRef(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Exec(`create table "Order Details" (OrderID integer not null, ProductID integer not null,
		Quantity integer, primary key (OrderID, ProductID))`); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{`"Order Details"`, "[Order Details]", "`Order Details`"} {
		tbl, err := db.Table(ref)
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if len(tbl.Columns) != 3 || len(tbl.TableColumns) != 3 {
			t.Errorf("%s: %d columns, %d table columns", ref, len(tbl.Columns), len(tbl.TableColumns))
		}
		pk, err := db.PKColNames(ref)
		if err != nil || !reflect.DeepEqual(pk, []string{"OrderID", "ProductID"}) {
			t.Errorf("%s: PK %v %v", ref, pk, err)
		}
	}
	if _, err := db.Table("Order Details"); err == nil {
		t.Error("an unquoted name with a space is not a table name in SQL")
	}
}
