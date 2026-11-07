package skeleton

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/datastore"
)

// TestCppTypeMapTargets: every target of the C++ type map of init is a class of the data store of
// that database, so a generated field never has a type the compiler does not know.
func TestCppTypeMapTargets(t *testing.T) {
	target := regexp.MustCompile(`target="([^"]+)"`)
	for _, db := range datastore.DBs {
		tm, err := files.ReadFile("files/typemap_cpp_" + db + ".xml")
		if err != nil {
			t.Fatal(err)
		}
		h, err := os.ReadFile(filepath.Join("..", "..", "datastore", "cpp", db, "DataStore.h"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range target.FindAllStringSubmatch(string(tm), -1) {
			base := strings.TrimSuffix(m[1], "${N}")
			names := []string{base}
			if base != m[1] {
				names = append(names, base+"N")
			}
			for _, n := range names {
				if !regexp.MustCompile(`(class|typedef [^;]*) ` + n + `\b`).Match(h) {
					t.Errorf("%s: %s of the type map is not declared in DataStore.h", db, n)
				}
			}
		}
	}
}
