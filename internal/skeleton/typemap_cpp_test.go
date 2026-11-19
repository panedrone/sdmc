package skeleton

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/panedrone/sdmc/datastore"
)

// TestCppTypeMapTargets: every target of the C++ type map of init is a type the compiler knows: a pair
// "${nullable}|0:<value>|1:<class>" has a Qt or C++ value and a class of the data store of that database,
// any other target is a class of that data store.
func TestCppTypeMapTargets(t *testing.T) {
	target := regexp.MustCompile(`target="([^"]+)"`)
	pair := regexp.MustCompile(`^\$\{nullable\}\|0:([^|]+)\|1:([^|]+)$`)
	values := map[string]bool{"qint16": true, "qint32": true, "qint64": true, "quint16": true, "quint32": true,
		"quint64": true, "float": true, "double": true, "bool": true, "QString": true, "QByteArray": true,
		"QDate": true, "QTime": true, "QDateTime": true}
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
			class := m[1]
			if strings.Contains(class, "$") || strings.Contains(class, "|") {
				p := pair.FindStringSubmatch(class)
				if p == nil {
					t.Errorf("%s: %s of the type map is not ${nullable}|0:<value>|1:<class>", db, class)
					continue
				}
				if !values[p[1]] {
					t.Errorf("%s: %s of the type map is not a value type", db, p[1])
				}
				class = p[2]
			}
			if !regexp.MustCompile(`(class|typedef [^;]*) ` + class + `\b`).Match(h) {
				t.Errorf("%s: %s of the type map is not declared in DataStore.h", db, class)
			}
		}
	}
}
