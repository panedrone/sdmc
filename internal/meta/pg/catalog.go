package pg

import (
	"github.com/jackc/pgx/v5"

	"github.com/panedrone/sdmc/internal/meta"
)

var _ meta.Catalog = (*DB)(nil)

// Tables: as pgjdbc getTables with no schema: all schemas but the system ones, tables
// ('r', 'p') before views, then by schema and name.
func (db *DB) Tables(includeViews bool) ([]string, error) {
	rows, err := db.conn.Query(db.ctx, `select c.relname
		from pg_catalog.pg_class c
		join pg_catalog.pg_namespace n on n.oid = c.relnamespace
		where (c.relkind in ('r', 'p') or $1 and c.relkind = 'v')
		  and n.nspname not like 'pg\_%' and n.nspname <> 'information_schema'
		order by c.relkind = 'v', n.nspname, c.relname`, includeViews)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (db *DB) ImportedKeys(table string) ([]meta.ImportedKey, error) {
	rows, err := db.conn.Query(db.ctx, `select pc.relname, fa.attname
		from pg_catalog.pg_constraint con
		join pg_catalog.pg_class fc on fc.oid = con.conrelid
		join pg_catalog.pg_namespace n on n.oid = fc.relnamespace
		join pg_catalog.pg_class pc on pc.oid = con.confrelid
		join pg_catalog.pg_namespace pn on pn.oid = pc.relnamespace
		cross join lateral unnest(con.conkey) with ordinality as k(attnum, pos)
		join pg_catalog.pg_attribute fa on fa.attrelid = con.conrelid and fa.attnum = k.attnum
		where con.contype = 'f' and fc.relname = $1
		  and n.nspname not like 'pg\_%' and n.nspname <> 'information_schema'
		order by pn.nspname, pc.relname, con.conname, k.pos`, table)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (meta.ImportedKey, error) {
		var k meta.ImportedKey
		err := r.Scan(&k.PKTable, &k.FKColumn)
		return k, err
	})
}
