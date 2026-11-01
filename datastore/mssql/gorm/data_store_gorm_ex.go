//go:build ignore

// Your part of the data store for gorm and SQL Server: the connection. See data_store_gorm.go.

package dbal

import (
	"context"
	"os"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func (ds *_DS) initDb() (err error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" { // sqlserver://user:pwd@host:1433?database=db, instance: host/SQLEXPRESS
		dsn = "sqlserver://sa:sa@localhost:1433?database=master"
	}
	dialector := sqlserver.Open(dsn)
	ds.rootDb, err = gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
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
