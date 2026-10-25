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
