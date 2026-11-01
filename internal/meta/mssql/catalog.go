package mssql

import "github.com/panedrone/sdmc/internal/meta"

var _ meta.Catalog = (*DB)(nil)

// IdentifierQuote: [] of SQL Server (meta.QuoteName), not the " of getIdentifierQuoteString.
func (db *DB) IdentifierQuote() string {
	return "["
}

// Tables: as mssql-jdbc getTables with no schema: all schemas but sys and INFORMATION_SCHEMA, no objects
// shipped by Microsoft, tables before views, then by schema and name.
func (db *DB) Tables(includeViews bool) ([]string, error) {
	rows, err := db.db.QueryContext(db.ctx, `select o.name from sys.objects o
		join sys.schemas s on s.schema_id = o.schema_id
		where (o.type = 'U' or @p1 = 1 and o.type = 'V') and o.is_ms_shipped = 0
		  and s.name not in ('sys', 'INFORMATION_SCHEMA')
		order by case when o.type = 'U' then 0 else 1 end, s.name, o.name`, includeViews)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		res = append(res, s)
	}
	return res, rows.Err()
}

// ImportedKeys: getImportedKeys of the table: by PK schema and table, then key order.
func (db *DB) ImportedKeys(table string) ([]meta.ImportedKey, error) {
	rows, err := db.db.QueryContext(db.ctx, `select pt.name, fc.name
		from sys.foreign_key_columns k
		join sys.objects ft on ft.object_id = k.parent_object_id
		join sys.objects pt on pt.object_id = k.referenced_object_id
		join sys.schemas ps on ps.schema_id = pt.schema_id
		join sys.columns fc on fc.object_id = k.parent_object_id and fc.column_id = k.parent_column_id
		where ft.name = @p1
		order by ps.name, pt.name, k.constraint_object_id, k.constraint_column_id`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []meta.ImportedKey
	for rows.Next() {
		var k meta.ImportedKey
		if err := rows.Scan(&k.PKTable, &k.FKColumn); err != nil {
			return nil, err
		}
		res = append(res, k)
	}
	return res, rows.Err()
}
