package sqlite

import (
	"github.com/ncruces/go-sqlite3"

	"github.com/panedrone/sdmc/internal/meta"
)

var _ meta.Catalog = (*DB)(nil)

// Tables: as xerial getTables: no sqlite_* tables, TABLE before VIEW, then by name.
func (db *DB) Tables(includeViews bool) ([]string, error) {
	views := "0"
	if includeViews {
		views = "1"
	}
	res := []string{}
	err := db.query(`select name from sqlite_master
		where name not like 'sqlite\_%' escape '\' and (type = 'table' or ? = '1' and type = 'view')
		order by upper(type), name`,
		[]string{views}, func(s *sqlite3.Stmt) { res = append(res, s.ColumnText(0)) })
	return res, err
}

// ImportedKeys: id of pragma_foreign_key_list counts the constraints backwards, so "id desc"
// is the order of declaration.
func (db *DB) ImportedKeys(table string) ([]meta.ImportedKey, error) {
	res := []meta.ImportedKey{}
	err := db.query(`select "table", "from" from pragma_foreign_key_list(?) order by "table", id desc, seq`,
		[]string{table}, func(s *sqlite3.Stmt) {
			res = append(res, meta.ImportedKey{PKTable: s.ColumnText(0), FKColumn: s.ColumnText(1)})
		})
	return res, err
}
