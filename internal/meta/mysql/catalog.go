package mysql

import "github.com/panedrone/sdmc/internal/meta"

var _ meta.Catalog = (*DB)(nil)

// Tables: as Connector/J getTables on the current database: BASE TABLE before VIEW, then by name.
func (db *DB) Tables(includeViews bool) ([]string, error) {
	views := 0
	if includeViews {
		views = 1
	}
	r, err := db.conn.Execute(`select TABLE_NAME from information_schema.TABLES
		where TABLE_SCHEMA = DATABASE() and (TABLE_TYPE = 'BASE TABLE' or ? = 1 and TABLE_TYPE = 'VIEW')
		order by TABLE_TYPE, TABLE_NAME`, views)
	if err != nil {
		return nil, err
	}
	res := make([]string, 0, r.RowNumber())
	for i := 0; i < r.RowNumber(); i++ {
		s, _ := r.GetString(i, 0)
		res = append(res, s)
	}
	return res, nil
}

func (db *DB) ImportedKeys(table string) ([]meta.ImportedKey, error) {
	r, err := db.conn.Execute(`select REFERENCED_TABLE_NAME, COLUMN_NAME
		from information_schema.KEY_COLUMN_USAGE
		where TABLE_SCHEMA = DATABASE() and TABLE_NAME = ? and REFERENCED_TABLE_NAME is not null
		order by REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION`, table)
	if err != nil {
		return nil, err
	}
	res := make([]meta.ImportedKey, 0, r.RowNumber())
	for i := 0; i < r.RowNumber(); i++ {
		t, _ := r.GetString(i, 0)
		c, _ := r.GetString(i, 1)
		res = append(res, meta.ImportedKey{PKTable: t, FKColumn: c})
	}
	return res, nil
}
