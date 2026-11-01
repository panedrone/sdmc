package meta

import (
	"reflect"
	"testing"
)

func TestSplitTableRef(t *testing.T) {
	for ref, want := range map[string][]string{
		"tasks":                 {"tasks"},
		"public.tasks":          {"public", "tasks"},
		"[Order Details]":       {"[Order Details]"},
		"[dbo].[Order Details]": {"[dbo]", "[Order Details]"},
		`"a.b".c`:               {`"a.b"`, "c"},
		"`x y`":                 {"`x y`"},
		"[a]]b].c":              {"[a]]b]", "c"}, // ]] inside of a quoted name
	} {
		got, ok := SplitTableRef(ref)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q %t, want %q", ref, got, ok, want)
		}
	}
	for _, ref := range []string{"[Order Details", `"a`, "`a"} {
		if _, ok := SplitTableRef(ref); ok {
			t.Errorf("%s: a quote is not closed", ref)
		}
	}
}

func TestUnquoteTableRef(t *testing.T) {
	for ref, want := range map[string]string{
		"tasks":                 "tasks",
		"[Order Details]":       "Order Details",
		"[dbo].[Order Details]": "dbo.Order Details",
		`"Order Details"`:       "Order Details",
		"`Order Details`":       "Order Details",
		`"a""b"`:                `a"b`,
		"[a]]b]":                "a]b",
		"[unclosed":             "[unclosed",
	} {
		if got := UnquoteTableRef(ref); got != want {
			t.Errorf("%s: %q, want %q", ref, got, want)
		}
	}
}

func TestQuoteName(t *testing.T) {
	for _, c := range []struct{ name, quote, want string }{
		{"Orders", "[", "Orders"},
		{"order_items", `"`, "order_items"},
		{"Café", `"`, "Café"}, // letters of any language are plain
		{"Order Details", "[", "[Order Details]"},
		{"a]b", "[", "[a]]b]"},
		{"Order Details", `"`, `"Order Details"`},
		{"api-key", "`", "`api-key`"},
		{"Order Details", "", "Order Details"}, // no quoting
	} {
		if got := QuoteName(c.name, c.quote); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.name, c.quote, got, c.want)
		}
	}
}
