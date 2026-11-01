//go:build ignore

// Your part of the data store for sqlx and SQL Server: the connection. See data_store_sqlx.go.

package dbal

import (
	"context"
	"os"

	"github.com/jmoiron/sqlx"
	_ "github.com/microsoft/go-mssqldb" // SQL Server
)

func (ds *_DS) initDb() (err error) {
	url := os.Getenv("DATABASE_URL") // sqlserver://user:pwd@host:1433?database=db, instance: host/SQLEXPRESS
	if url == "" {
		url = "sqlserver://sa:sa@localhost:1433?database=master"
	}
	ds.db, err = sqlx.Open("sqlserver", url)
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
