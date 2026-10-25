package assist

import (
	"errors"
	"testing"

	"github.com/panedrone/sdmc/internal/cg"
	"github.com/panedrone/sdmc/internal/meta"
)

type fakeCatalog struct {
	tables []string
	keys   map[string][]meta.ImportedKey
	keyErr map[string]bool
}

func (c fakeCatalog) Tables(includeViews bool) ([]string, error) {
	if !includeViews {
		return nil, errors.New("views expected")
	}
	return c.tables, nil
}

func (c fakeCatalog) ImportedKeys(table string) ([]meta.ImportedKey, error) {
	if c.keyErr[table] {
		return nil, errors.New("no access")
	}
	return c.keys[table], nil
}

var shop = fakeCatalog{
	tables: []string{"categories", "order_items", "orders", "v_sales"},
	keys: map[string][]meta.ImportedKey{
		// two FK columns to one table go to one method, as in Java
		"order_items": {
			{PKTable: "orders", FKColumn: "order_id"},
			{PKTable: "products", FKColumn: "product_id"},
			{PKTable: "products", FKColumn: "alt_product_id"},
		},
		"orders": {{PKTable: "categories", FKColumn: "category_id"}},
	},
	keyErr: map[string]bool{"v_sales": true},
}

func TestDtoClasses(t *testing.T) {
	got, err := DtoClasses(shop, "", map[string]bool{"Order": true})
	if err != nil {
		t.Fatal(err)
	}
	want := `<dto-class name="Category" ref="categories"/>
<dto-class name="OrderItem" ref="order_items"/>
<dto-class name="VSale" ref="v_sales"/>
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDaoClasses(t *testing.T) {
	got, err := DaoClasses(shop, "gorm", cg.LowerCamelCase, map[string]bool{"Category": true}, map[string]bool{"OrdersDao": true})
	if err != nil {
		t.Fatal(err)
	}
	want := `<dao-class name="OrderItemsDao">
    <crud dto="gorm-OrderItem" table="order_items"/>
    <query-dto-list dto="gorm-OrderItem" method="getOrderItemsByOrder(orderID)" ref="order_items(order_id)"/>
    <query-dto-list dto="gorm-OrderItem" method="getOrderItemsByProduct(productID, altProductID)" ref="order_items(product_id, alt_product_id)"/>
</dao-class>
<dao-class name="VSalesDao">
    <crud dto="gorm-VSale" table="v_sales"/>
</dao-class>
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Python: the FK access in snake_case, the DTO classes without a model.
func TestDaoClassesPython(t *testing.T) {
	got, err := DaoClasses(shop, "", cg.SnakeCase, map[string]bool{"Category": true}, map[string]bool{"OrdersDao": true, "VSalesDao": true})
	if err != nil {
		t.Fatal(err)
	}
	want := `<dao-class name="OrderItemsDao">
    <crud dto="OrderItem" table="order_items"/>
    <query-dto-list dto="OrderItem" method="get_order_items_by_order(order_id)" ref="order_items(order_id)"/>
    <query-dto-list dto="OrderItem" method="get_order_items_by_product(product_id, alt_product_id)" ref="order_items(product_id, alt_product_id)"/>
</dao-class>
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// C++: the FK access as is, the names of the columns.
func TestDaoClassesCpp(t *testing.T) {
	got, err := DaoClasses(shop, "", cg.AsIs, map[string]bool{"Category": true}, map[string]bool{"OrdersDao": true, "VSalesDao": true})
	if err != nil {
		t.Fatal(err)
	}
	want := `<dao-class name="OrderItemsDao">
    <crud dto="OrderItem" table="order_items"/>
    <query-dto-list dto="OrderItem" method="getOrderItemsByOrder(order_id)" ref="order_items(order_id)"/>
    <query-dto-list dto="OrderItem" method="getOrderItemsByProduct(product_id, alt_product_id)" ref="order_items(product_id, alt_product_id)"/>
</dao-class>
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDtoClassName(t *testing.T) {
	for table, want := range map[string]string{
		"tasks":           "Task",
		"TASKS":           "Task",
		"order_items":     "OrderItem",
		"categories":      "Category",
		"boxes":           "Box",
		"addresses":       "Address",
		"branches":        "Branch",
		"people_children": "PeopleChild",
		"news":            "New", // a presumed English noun, as in Java
		"status":          "Statu",
		"data":            "Datum",
		"sheep":           "Sheep",
		"user_class":      "UserClass",
		"123":             "123",
	} {
		if got := DtoClassName(table, true); got != want {
			t.Errorf("%s: %s, want %s", table, got, want)
		}
	}
	if got := DtoClassName("order_items", false); got != "OrderItems" {
		t.Errorf("plural kept: %s", got)
	}
	if got := DaoClassName("order_items"); got != "OrderItemsDao" {
		t.Errorf("DAO class: %s", got)
	}
}
