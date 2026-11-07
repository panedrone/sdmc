package pg

import "testing"

func TestHelpers(t *testing.T) {
	if got := sqlType(23); got != 4 { // int4 -> INTEGER
		t.Errorf("int4: %d", got)
	}
	if got := sqlType(999999); got != sqlTypeOther {
		t.Errorf("unknown oid: %d", got)
	}
	for typmod, want := range map[int32]int{-1: 7, 0: 0, 1: 3, 3: 4} {
		if got := secondSize(typmod); got != want {
			t.Errorf("secondSize(%d) = %d, want %d", typmod, got, want)
		}
	}
	if s, tb, err := splitSchema("public.t"); s != "public" || tb != "t" || err != nil {
		t.Errorf("public.t: %q %q %v", s, tb, err)
	}
	if s, tb, err := splitSchema("t"); s != "" || tb != "t" || err != nil {
		t.Errorf("t: %q %q %v", s, tb, err)
	}
	if _, _, err := splitSchema("a.b.c"); err == nil {
		t.Error("a.b.c: no error")
	}
	db := &DB{}
	if db.IdentifierQuote() != `"` {
		t.Error("quote")
	}
	if db.RawOfJava(" java.lang.Integer ") != "int4" || db.RawOfJava("x.Unknown") != "" {
		t.Error("RawOfJava")
	}
}

// resolve and pgType do not query the server for the types they know.
func TestResolveKnown(t *testing.T) {
	db := &DB{types: map[uint32]pgType{1007: {name: "_int4", isArray: true, elem: 23}, 99999: {name: "mytype"}}}
	if j, oid, err := db.resolve(23); j != "java.lang.Integer" || oid != 23 || err != nil {
		t.Errorf("int4: %q %d %v", j, oid, err)
	}
	if j, oid, err := db.resolve(1007); j != javaArray || oid != 23 || err != nil {
		t.Errorf("_int4: %q %d %v", j, oid, err)
	}
	if j, oid, err := db.resolve(99999); j != javaObject || oid != 99999 || err != nil {
		t.Errorf("mytype: %q %d %v", j, oid, err)
	}
}
