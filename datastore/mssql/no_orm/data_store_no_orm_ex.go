//go:build ignore

// Your part of the data store for database/sql (no ORM) and SQL Server: the connection. See data_store_no_orm.go.

package dbal

import (
	"context"
	"database/sql"
	"os"

	_ "github.com/microsoft/go-mssqldb" // SQL Server
)

func (ds *_DS) initDb() (err error) {
	url := os.Getenv("DATABASE_URL") // sqlserver://user:pwd@host:1433?database=db, instance: host/SQLEXPRESS
	if url == "" {
		url = "sqlserver://sa:sa@localhost:1433?database=master"
	}
	ds.db, err = sql.Open("sqlserver", url)
	return
}

var _ds = &_DS{}

func Ds() DataStore {
	return _ds
}

func init() {
	// generated DAO functions get DataStore via DS(ctx)
	DS = func(ctx context.Context) DataStore {
		return _ds
	}
}
