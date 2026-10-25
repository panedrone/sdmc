//go:build ignore

// The data store for gorm and SQLite, used by the generated DAO.
// "sdmc init -orm gorm -db sqlite" writes it to the DAO folder. A copy by hand: this file and
// data_store_gorm_ex.go from the same folder, without the line "//go:build ignore" and with
// the package name of the DAO folder.

package dbal

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"time"

	"gorm.io/gorm"
)

var DS func(context context.Context) DataStore

/*
	This file is a part of SQL DAL Maker Project: https://sqldalmaker.sourceforge.net
	It demonstrates how to implement an interface DataStore in Go + Gorm.
	More about DataStore: https://sqldalmaker.sourceforge.net/preconfig.html#ds
	Recent version: https://github.com/panedrone/sqldalmaker/blob/master/src/resources/data_store_gorm.go

	Successfully tested with:

		- "gorm.io/driver/sqlite"
		- "gorm.io/driver/postgres"
		- "gorm.io/driver/mysql"

	Copy-paste this code to your project and change it for your needs.
	Improvements are welcome: sqldalmaker@gmail.com

	Demo project: https://github.com/panedrone/sdm_todolist_go_react_16_npm_sqlite3
*/

type DataStore interface {
	Session(ctx context.Context) *gorm.DB

	Open() error
	Close() error

	Begin(ctx context.Context) (txCtx context.Context, err error)
	Commit(txCtx *context.Context) error
	Rollback(txCtx *context.Context) error

	// CRUD

	Create(ctx context.Context, table string, dataObjRef any) error
	ReadAll(ctx context.Context, table string, sliceOfDataObjRef any) error
	Read(ctx context.Context, table string, dataObjRef any, pk ...any) error
	Update(ctx context.Context, table string, dataObjRef any) (rowsAffected int64, err error)
	Delete(ctx context.Context, table string, dataObjRef any) (rowsAffected int64, err error)

	// raw-SQL

	Exec(ctx context.Context, sqlString string, args ...any) (res int64, err error)

	Query(ctx context.Context, sqlString string, destPtr any, args ...any) error
	QueryAll(ctx context.Context, sqlString string, destSlicePtr any, args ...any) error
	QueryRow(ctx context.Context, sqlString string, args ...any) (data map[string]any, err error)
	QueryAllRows(ctx context.Context, sqlString string, onRow func(map[string]any), args ...any) error

	Select(ctx context.Context, sqlString string, fa any, args ...any) error
}

type _DS struct {
	rootDb *gorm.DB
}

func _close(obj io.Closer) {
	_ = obj.Close()
}

// Open — initDb is in data_store_gorm_ex.go
func (ds *_DS) Open() error {
	return ds.initDb()
}

func (ds *_DS) Close() error {
	// https://stackoverflow.com/questions/63816057/how-do-i-close-database-instance-in-gorm-1-20-0
	sqlDB, err := ds.rootDb.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	if err != nil {
		return err
	}
	return nil
}

const txKey = "tx"

func (ds *_DS) gormTx(ctx context.Context) *gorm.DB {
	tx, _ := ctx.Value(txKey).(*gorm.DB)
	return tx
}

func (ds *_DS) Begin(ctx context.Context) (txCtx context.Context, err error) {
	tx := ds.gormTx(ctx)
	if tx != nil {
		return nil, errors.New("already in " + txKey)
	}
	tx = ds.rootDb.Begin()
	txCtx = context.WithValue(ctx, txKey, tx)
	return
}

func (ds *_DS) Commit(txCtx *context.Context) (err error) {
	if txCtx == nil {
		return errNilParam()
	}
	tx := ds.gormTx(*txCtx)
	if tx == nil {
		return errNotInTx()
	}
	tx.Commit()
	*txCtx = nil
	return
}

func (ds *_DS) Rollback(txCtx *context.Context) (err error) {
	if txCtx == nil {
		return errNilParam()
	}
	tx := ds.gormTx(*txCtx)
	if tx == nil {
		return errNotInTx()
	}
	tx.Rollback()
	*txCtx = nil
	return
}

// ORM-based CRUD -----------------------------------

func (ds *_DS) Session(ctx context.Context) *gorm.DB {
	tx := ds.gormTx(ctx)
	if tx == nil {
		return ds.rootDb.WithContext(ctx)
	}
	return tx.WithContext(ctx)
}

func (ds *_DS) Create(ctx context.Context, table string, dataObjRef any) error {
	return ds.Session(ctx).Table(table).Create(dataObjRef).Error
}

func (ds *_DS) ReadAll(ctx context.Context, table string, sliceOfDataObjRef any) error {
	return ds.Session(ctx).Table(table).Find(sliceOfDataObjRef).Error
}

func (ds *_DS) Read(ctx context.Context, table string, dataObjRef any, pk ...any) error {
	return ds.Session(ctx).Table(table).Take(dataObjRef, pk...).Error
}

func (ds *_DS) Update(ctx context.Context, table string, dataObjRef any) (rowsAffected int64, err error) {
	db := ds.Session(ctx).Table(table).Updates(dataObjRef)
	err = db.Error
	rowsAffected = db.RowsAffected
	return
}

func (ds *_DS) Delete(ctx context.Context, table string, dataObjRef any) (rowsAffected int64, err error) {
	db := ds.Session(ctx).Table(table).Delete(dataObjRef)
	err = db.Error
	rowsAffected = db.RowsAffected
	return
}

// raw-SQL --------------------------------

func (ds *_DS) rawBuild(ctx context.Context, sqlString string, args ...any) *gorm.DB {
	return ds.Session(ctx).Raw(sqlString, args...)
}

func (ds *_DS) rawQuery(ctx context.Context, sqlString string, args ...any) (*sql.Rows, error) {
	return ds.rawBuild(ctx, sqlString, args...).Rows()
}

func (ds *_DS) rawExec(ctx context.Context, sqlString string, args ...any) (rowsAffected int64, err error) {
	res := ds.Session(ctx).Exec(sqlString, args...)
	return res.RowsAffected, res.Error
}

func (ds *_DS) Exec(ctx context.Context, sqlString string, args ...any) (rowsAffected int64, err error) {
	sqlString = ds.formatSQL(sqlString)
	return ds.rawExec(ctx, sqlString, args...)
}

func (ds *_DS) Query(ctx context.Context, sqlString string, destPtr any, args ...any) error {
	sqlString = ds.formatSQL(sqlString)
	rows, err := ds.rawQuery(ctx, sqlString, args...)
	if err != nil {
		return err
	}
	defer _close(rows)
	_, _, values, valuePointers, err := ds.prepareFetch(rows)
	if err != nil {
		return err
	}
	if !rows.Next() {
		return errNoRows(sqlString)
	}
	err = rows.Scan(valuePointers...)
	if err != nil {
		return err
	}
	err = SetRes(destPtr, values)
	if err != nil {
		return err
	}
	if rows.Next() {
		return errMultipleRows(sqlString)
	}
	return nil
}

func (ds *_DS) QueryAll(ctx context.Context, sqlString string, destSlicePtr any, args ...any) (err error) {
	sqlString = ds.formatSQL(sqlString)
	rows, err := ds.rawQuery(ctx, sqlString, args...)
	if err != nil {
		return
	}
	defer _close(rows)
	err = ds.selectAllScalars(rows, destSlicePtr)
	return
}

func (ds *_DS) selectAllScalars(rows *sql.Rows, dest any) error {
	base, direct, elemIsPtr, err := prepareSelectScalars(dest)
	if err != nil {
		return err
	}
	errMap := make(map[string]int)
	for {
		// re-detect columns for each ResultSet
		// fetch all columns! if to fetch less, Scan returns nil-s
		_, _, values, valuePointers, pfErr := ds.prepareFetch(rows)
		if pfErr != nil {
			return pfErr
		}
		for rows.Next() {
			err = rows.Scan(valuePointers...)
			if err != nil {
				return err
			}
			vp := reflect.New(base)
			dataPtr := vp.Interface()
			SetScalarValue(dataPtr, values, errMap)
			// append
			if elemIsPtr {
				direct.Set(reflect.Append(direct, vp))
			} else {
				direct.Set(reflect.Append(direct, reflect.Indirect(vp)))
			}
		}
		if !rows.NextResultSet() {
			break
		}
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	err = ErrMapToErr(errMap)
	return err
}

func prepareSelectScalars(dest any) (base reflect.Type, direct reflect.Value, elemIsPtr bool, err error) {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Ptr {
		err = errors.New("must pass a pointer, not a value")
		return
	}
	if value.IsNil() {
		err = errors.New("nil pointer passed")
		return
	}
	direct = reflect.Indirect(value)
	slice, errBase := baseType(value.Type(), reflect.Slice)
	if errBase != nil {
		err = errBase
		return
	}
	direct.SetLen(0)
	elemIsPtr = slice.Elem().Kind() == reflect.Ptr
	base = Deref(slice.Elem())
	return
}

func baseType(t reflect.Type, expected reflect.Kind) (reflect.Type, error) {
	t = Deref(t)
	if t.Kind() != expected {
		return nil, fmt.Errorf("expected %s but got %s", expected, t.Kind())
	}
	return t, nil
}

// Deref is Indirect for reflect.Types
func Deref(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func (ds *_DS) QueryRow(ctx context.Context, sqlString string, args ...any) (row map[string]any, err error) {
	sqlString = ds.formatSQL(sqlString)
	rows, err := ds.rawQuery(ctx, sqlString, args...)
	if err != nil {
		return
	}
	defer _close(rows)
	colNames, row, values, valuePointers, pfErr := ds.prepareFetch(rows)
	if pfErr != nil {
		err = pfErr
		return
	}
	if !rows.Next() {
		err = errNoRows(sqlString)
		return
	}
	err = rows.Scan(valuePointers...)
	if err != nil {
		return
	}
	for i, col := range colNames {
		row[col] = values[i]
	}
	if rows.Next() {
		err = errMultipleRows(sqlString)
		return
	}
	return
}

func (ds *_DS) QueryAllRows(ctx context.Context, sqlString string, onRow func(map[string]any), args ...any) (err error) {
	// many thanks to:
	// https://stackoverflow.com/questions/51731423/how-to-read-a-row-from-a-table-to-a-map-without-knowing-columns
	sqlString = ds.formatSQL(sqlString)
	rows, err := ds.rawQuery(ctx, sqlString, args...)
	if err != nil {
		return
	}
	defer _close(rows)
	for {
		// re-detect columns for each ResultSet
		colNames, data, values, valuePointers, pfErr := ds.prepareFetch(rows)
		if pfErr != nil {
			err = pfErr
			return
		}
		for rows.Next() {
			err = rows.Scan(valuePointers...)
			if err != nil {
				return
			}
			for i, col := range colNames {
				data[col] = values[i]
			}
			onRow(data)
		}
		if !rows.NextResultSet() {
			break
		}
	}
	return
}

func isPtrSlice(i any) bool {
	// https://stackoverflow.com/questions/69675420/how-to-check-if-interface-is-a-a-pointer-to-a-slice
	if i == nil {
		return false
	}
	v := reflect.ValueOf(i)
	if v.Kind() != reflect.Ptr {
		return false
	}
	return v.Elem().Kind() == reflect.Slice
}

func (ds *_DS) Select(ctx context.Context, sqlString string, dest any, args ...any) error {
	sqlString = ds.formatSQL(sqlString)
	faArr, ok := dest.([]any)
	if ok {
		rows, err := ds.rawQuery(ctx, sqlString, args...)
		if err != nil {
			return err
		}
		defer _close(rows)
		if !rows.Next() {
			return errNoRows(sqlString)
		}
		err = rows.Scan(faArr...)
		if err != nil {
			return err
		}
		if rows.Next() {
			return errMultipleRows(sqlString)
		}
		return nil
	}
	onRow, ok := dest.(func(func(any) error) error)
	if ok {
		rows, err := ds.rawQuery(ctx, sqlString, args...)
		if err != nil {
			return err
		}
		defer _close(rows)
		return fetchAll(rows, onRow)
	}
	raw := ds.rawBuild(ctx, sqlString, args...)
	err := raw.Error
	if err != nil {
		return err
	}
	if isPtrSlice(dest) {
		return raw.Find(dest).Error
	}
	return raw.Take(dest).Error
}

func fetchAll(rows *sql.Rows, onRow func(func(any) error) error) (err error) {
	scan := func(fa any) error {
		faArr, ok := fa.([]any)
		if !ok {
			return errUnexpectedType(fa)
		}
		return rows.Scan(faArr...)
	}
	for {
		for rows.Next() {
			err = onRow(scan)
			if err != nil {
				return
			}
		}
		if !rows.NextResultSet() {
			return
		}
	}
}

func (ds *_DS) prepareFetch(rows *sql.Rows) (colNames []string, data map[string]any, values []any, valuePointers []any, err error) {
	colNames, err = rows.Columns()
	if err != nil {
		return
	}
	data = make(map[string]any)
	// any is ok for SQLite3.
	// MySQL and PostgreSQL may require some convertors from []uint8
	// https://github.com/ziutek/mymysql#type-mapping
	values = make([]any, len(colNames))
	valuePointers = make([]any, len(colNames))
	for i := range values {
		valuePointers[i] = &values[i]
	}
	return
}

// formatSQL — gorm takes ? for every database
func (ds *_DS) formatSQL(sqlString string) string {
	return sqlString
}

/////////////////////////////////////////////////////////////

func SetString(d *string, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setString(d, value)
	})
}

func _setString(d *string, value any) error {
	if value == nil {
		*d = ""
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*d = string(v)
	case int, int32, int64:
		*d = fmt.Sprintf("%v", v)
	case float64, float32:
		*d = fmt.Sprintf("%v", v) // %v prints just 0.12 instead of 0.120000
	case string:
		*d = v
	case time.Time:
		*d = v.Format("2006-01-02 15:04:05")
	default:
		return unknownTypeErr(d, value, "_setString")
	}
	return nil
}

func SetInt64(d *int64, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setInt64(d, value)
	})
}

func _setInt64(d *int64, value any) error {
	switch v := value.(type) {
	case int64:
		*d = v
	case int32:
		*d = int64(v) // MySQL
	case float64:
		*d = int64(v)
	case float32:
		*d = int64(v)
	case []byte:
		str := string(v)
		i64, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return assignErr(d, value, "_setInt64", err.Error())
		}
		*d = i64
	case string:
		str := value.(string)
		i64, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return assignErr(d, value, "_setInt64", err.Error())
		}
		*d = i64
	default:
		return unknownTypeErr(d, value, "_setInt64")
	}
	return nil
}

func SetInt(d *int, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setInt(d, value)
	})
}

func _setInt(d *int, value any) error {
	switch v := value.(type) {
	case int:
		*d = v
	case int32:
		*d = int(v)
	case int64:
		*d = int(v)
	case float64:
		*d = int(v)
	case float32:
		*d = int(v)
	case []byte:
		str := string(v)
		d64, err := strconv.ParseInt(str, 10, 32)
		if err != nil {
			return assignErr(d, value, "_setInt", err.Error())
		}
		*d = int(d64)
	case string:
		d64, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return assignErr(d, value, "_setInt", err.Error())
		}
		*d = int(d64)
	default:
		return unknownTypeErr(d, value, "_setInt")
	}
	return nil
}

func SetInt32(d *int32, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setInt32(d, value)
	})
}

func _setInt32(d *int32, value any) error {
	switch v := value.(type) {
	case int32:
		*d = v
	case int64:
		*d = int32(v)
	case float64:
		*d = int32(v)
	case float32:
		*d = int32(v)
	case []byte:
		str := string(v)
		d64, err := strconv.ParseInt(str, 10, 32)
		if err != nil {
			return assignErr(d, value, "_setInt32", err.Error())
		}
		*d = int32(d64)
	case string:
		d64, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return assignErr(d, value, "_setInt32", err.Error())
		}
		*d = int32(d64)
	default:
		return unknownTypeErr(d, value, "_setInt32")
	}
	return nil
}

func SetFloat32(d *float32, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setFloat32(d, value)
	})
}

func _setFloat32(d *float32, value any) error {
	switch v := value.(type) {
	case float32:
		*d = v
	case float64:
		*d = float32(v)
	case []byte:
		str := string(v) // PostgreSQL
		d64, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return assignErr(d, value, "_setFloat32", err.Error())
		}
		*d = float32(d64)
	case string:
		d64, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return assignErr(d, value, "_setFloat32", err.Error())
		}
		*d = float32(d64)
	default:
		return unknownTypeErr(d, value, "_setFloat32")
	}
	return nil
}

func SetFloat64(d *float64, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setFloat64(d, value)
	})
}

func _setFloat64(d *float64, value any) error {
	switch v := value.(type) {
	case float64:
		*d = v
	case float32:
		*d = float64(v)
	case int64:
		*d = float64(v) // SQLite: REAL/NUMERIC column with an integer value
	case []byte:
		str := string(v) // PostgreSQL, MySQL
		var err error
		*d, err = strconv.ParseFloat(str, 64)
		if err != nil {
			return assignErr(d, value, "_setFloat64", err.Error())
		}
	case string:
		var err error
		*d, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return assignErr(d, value, "_setFloat64", err.Error())
		}
	default:
		return unknownTypeErr(d, value, "_setFloat64")
	}
	return nil
}

func SetTime(d *time.Time, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setTime(d, value)
	})
}

func _setTime(d *time.Time, value any) error {
	switch v := value.(type) {
	case time.Time:
		*d = v
	default:
		return unknownTypeErr(d, value, "_setTime")
	}
	return nil
}

func SetBool(d *bool, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setBool(d, value)
	})
}

func _setBool(d *bool, value any) error {
	switch v := value.(type) {
	case []byte:
		str := string(v) // MySQL
		db, err := strconv.ParseBool(str)
		if err != nil {
			return assignErr(d, value, "_setBool", err.Error())
		}
		*d = db
	case bool:
		*d = v
	default:
		return unknownTypeErr(d, value, "_setBool")
	}
	return nil
}

func SetBytes(d *[]byte, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _setBytes(d, value)
	})
}

func _setBytes(d *[]byte, value any) error {
	switch v := value.(type) {
	case []byte:
		*d = v
	default:
		return unknownTypeErr(d, value, "_setBytes")
	}
	return nil
}

func SetNum(d any, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		s, ok := d.(sql.Scanner)
		if ok {
			return _scan(s, value)
		}
		return _setAny(d, value)
	})
}

func Scan(d sql.Scanner, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		return _scan(d, value)
	})
}

func _scan(d sql.Scanner, value any) error {
	err := d.Scan(value)
	if err != nil {
		return assignErr(d, value, "_scan", err.Error())
	}
	return nil
}

func _copy(row map[string]any, col string, errMap map[string]int, fn func(value any) error) {
	var ok bool
	value, ok := row[col]
	if !ok {
		key := fmt.Sprintf("[%s] column not found", col)
		count, ok := errMap[key]
		if ok {
			errMap[key] = count + 1
		} else {
			errMap[key] = 1
		}
		return
	}
	if value == nil {
		return // NULL: the field keeps its zero value
	}
	err := fn(value)
	if err == nil {
		return
	}
	key := fmt.Sprintf("[%s] %s", col, err.Error())
	count, ok := errMap[key]
	if ok {
		errMap[key] = count + 1
	} else {
		errMap[key] = 1
	}
}

func _setAny(dstPtr any, value any) error {
	if value == nil {
		switch d := dstPtr.(type) {
		case *any:
			*d = nil
		}
		return nil // leave as-is
	}
	n, ok := value.(driver.Valuer)
	if ok {
		v, err := n.Value()
		if err == nil {
			err = _setAny(dstPtr, v)
		}
		return err
	}
	switch d := dstPtr.(type) {
	case *string:
		return _setString(d, value)
	case *int:
		return _setInt(d, value)
	case *int32:
		return _setInt32(d, value)
	case *int64:
		return _setInt64(d, value)
	case *float64:
		return _setFloat64(d, value)
	case *float32:
		return _setFloat32(d, value)
	case *time.Time:
		return _setTime(d, value)
	case *bool:
		return _setBool(d, value)
	case *[]byte: // the same as uint8
		return _setBytes(d, value)
	case sql.Scanner:
		return _scan(d, value)
	//case *[]string:
	//	switch bv := value.(type) {
	//	case []byte:
	//		sa := pq.StringArray{}
	//		err := sa.Scan(bv)
	//		if err != nil {
	//			return assignErr(d, value, "_setAny", err.Error())
	//		}
	//		*d = sa
	//		return nil
	//  default:
	//      return unknownTypeErr(d, value, "_setAny")
	//	}
	//case *pq.StringArray:
	//	switch bv := value.(type) {
	//	case []byte:
	//		err := d.Scan(bv)
	//		if err != nil {
	//			return assignErr(d, value, "_setAny", err.Error())
	//		}
	//		return nil
	//  default:
	//      return unknownTypeErr(d, value, "_setAny")
	//	}
	case *any:
		*d = value
		return nil
	}
	return errUnexpectedType(dstPtr)
}

func SetRes(dstPtr any, value any) error {
	var err error
	switch v := value.(type) {
	case []any:
		switch d := dstPtr.(type) {
		case *[]any:
			*d = v
		default:
			v0 := v[0]
			err = _setAny(dstPtr, v0)
		}
	default:
		err = _setAny(dstPtr, value)
	}
	return err
}

func SetScalarValue(dstPtr any, value any, errMap map[string]int) {
	err := SetRes(dstPtr, value)
	if err == nil {
		return
	}
	key := err.Error()
	count, ok := errMap[key]
	if ok {
		errMap[key] = count + 1
	} else {
		errMap[key] = 1
	}
}

func SetAny(dstPtr any, row map[string]any, col string, errMap map[string]int) {
	_copy(row, col, errMap, func(value any) error {
		SetScalarValue(dstPtr, value, errMap)
		return nil
	})
}

/////////////////////////////////////////////////////////////

func ErrMapToErr(errMap map[string]int) (err error) {
	if len(errMap) > 0 {
		err = fmt.Errorf("%v", errMap)
	}
	return
}

func assignErr(dstPtr any, value any, funcName string, errMsg string) error {
	return fmt.Errorf("%s %T <- %T %s", funcName, dstPtr, value, errMsg)
}

func unknownTypeErr(dstPtr any, value any, funcName string) error {
	return assignErr(dstPtr, value, funcName, "unknown type")
}

func errUnexpectedType(val any) error {
	return fmt.Errorf("unexpected type: %v", reflect.TypeOf(val))
}

func errNoRows(sqlString string) error {
	return fmt.Errorf("no rows found for %s", sqlString)
}

func errMultipleRows(sqlString string) error {
	return fmt.Errorf("more than 1 row found for %s", sqlString)
}

func errNotInTx() error {
	return errors.New(txKey + " not started")
}

func errNilParam() error {
	return errors.New("nil parameter")
}
