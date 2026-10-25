package sqlite

import (
	"os"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/internal/meta/metadump"
)

// TestGolden is a full run on in-memory SQLite: schema and queries from testdata/sqlite.txt,
// the expected output is that of MetaDump.java (xerial sqlite-jdbc 3.53) on the same file.
//
// To retake the reference (from sdm-plugins, after building the plug-in):
//
//	java -cp "<MetaDump classes>;out/production/sqldalmaker-intellij;sqlite-jdbc-3.53.2.1.jar" \
//	  com.sqldalmaker.cg.MetaDump "jdbc:sqlite::memory:" "" internal/meta/testdata/sqlite.txt
func TestGolden(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	in, err := os.Open("../testdata/sqlite.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	var got strings.Builder
	if err := metadump.Run(db, in, &got); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../testdata/sqlite_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	gotLines := strings.Split(got.String(), "\n")
	wantLines := strings.Split(strings.ReplaceAll(string(want), "\r\n", "\n"), "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Errorf("line %d:\n got  %s\n want %s", i+1, g, w)
		}
	}
}

func TestColumnType(t *testing.T) {
	cases := []struct {
		decl                 string
		dataType, size, digs int
	}{
		{"INTEGER", typeInteger, unknownSize, 0},
		{"BOOLEAN", typeInteger, unknownSize, 0},
		{"CHARINT", typeInteger, unknownSize, 0}, // INT is checked before CHAR
		{"DOUBLECHAR", typeVarchar, unknownSize, 0},
		{"BLOB", typeVarchar, unknownSize, 0},
		{"REAL", typeFloat, unknownSize, 10},
		{"DATE", typeVarchar, unknownSize, 10},
		{"", typeVarchar, unknownSize, 10},
		{"VARCHAR(50)", typeVarchar, 50, 0},
		{"DECIMAL(10,2)", typeFloat, 12, 2},
		{"NUMERIC( 8 , 3 )", typeFloat, 11, 3},
		{"DECIMAL(5)", typeFloat, 5, 0},
	}
	for _, c := range cases {
		d, s, g := columnType(c.decl)
		if d != c.dataType || s != c.size || g != c.digs {
			t.Errorf("%q: got %d/%d/%d, want %d/%d/%d", c.decl, d, s, g, c.dataType, c.size, c.digs)
		}
	}
}
