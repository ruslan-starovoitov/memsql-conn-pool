// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sql provides a generic interface around SQL (or SQL-like)
// databases.
//
// The sql package must be used in conjunction with a database driver.
// See https://golang.org/s/sqldrivers for a list of drivers.
//
// Drivers that do not support context cancellation will not return until
// after the query is completed.
//
// For usage examples, see the wiki page at
// https://golang.org/s/sqlwiki.
package cpool

import (
	"context"
	"cpool/driver"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
)

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]driver.Driver)
)

// nowFunc returns the current time; it's overridden in tests.
var nowFunc = time.Now

// Register makes a database driver available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(name string, driver driver.Driver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	if driver == nil {
		panic("sql: Register driver is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("sql: Register called twice for driver " + name)
	}
	drivers[name] = driver
}

func unregisterAllDrivers() {
	driversMu.Lock()
	defer driversMu.Unlock()
	// For tests.
	drivers = make(map[string]driver.Driver)
}

// Drivers returns a sorted list of the names of the registered drivers.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	list := make([]string, 0, len(drivers))
	for name := range drivers {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}

// A NamedArg is a named argument. NamedArg values may be used as
// arguments to Query or Exec and bind to the corresponding named
// parameter in the SQL statement.
//
// For a more concise way to create NamedArg values, see
// the Named function.
type NamedArg struct {
	_Named_Fields_Required struct{}

	// Name is the name of the parameter placeholder.
	//
	// If empty, the ordinal position in the argument list will be
	// used.
	//
	// Name must omit any symbol prefix.
	Name string

	// Value is the value of the parameter.
	// It may be assigned the same value types as the query
	// arguments.
	Value interface{}
}

// Named provides a more concise way to create NamedArg values.
//
// Example usage:
//
//     connPool.ExecContext(ctx, `
//         delete from Invoice
//         where
//             TimeCreated < @end
//             and TimeCreated >= @start;`,
//         sql.Named("start", startTime),
//         sql.Named("end", endTime),
//     )
func Named(name string, value interface{}) NamedArg {
	// This method exists because the go1compat promise
	// doesn't guarantee that structs don't grow more fields,
	// so unkeyed struct literals are a vet error. Thus, we don't
	// want to allow sql.NamedArg{name, value}.
	return NamedArg{Name: name, Value: value}
}

// IsolationLevel is the transaction isolation level used in TxOptions.
type IsolationLevel int

// Various isolation levels that drivers may support in BeginTx.
// If a driver does not support a given isolation level an error may be returned.
//
// See https://en.wikipedia.org/wiki/Isolation_(database_systems)#Isolation_levels.
const (
	LevelDefault IsolationLevel = iota
	LevelReadUncommitted
	LevelReadCommitted
	LevelWriteCommitted
	LevelRepeatableRead
	LevelSnapshot
	LevelSerializable
	LevelLinearizable
)

// String returns the name of the transaction isolation level.
func (i IsolationLevel) String() string {
	switch i {
	case LevelDefault:
		return "Default"
	case LevelReadUncommitted:
		return "Read Uncommitted"
	case LevelReadCommitted:
		return "Read Committed"
	case LevelWriteCommitted:
		return "Write Committed"
	case LevelRepeatableRead:
		return "Repeatable Read"
	case LevelSnapshot:
		return "Snapshot"
	case LevelSerializable:
		return "Serializable"
	case LevelLinearizable:
		return "Linearizable"
	default:
		return "IsolationLevel(" + strconv.Itoa(int(i)) + ")"
	}
}

var _ fmt.Stringer = LevelDefault

// TxOptions holds the transaction options to be used in ConnPool.BeginTx.
type TxOptions struct {
	// Isolation is the transaction isolation level.
	// If zero, the driver or database's default level is used.
	Isolation IsolationLevel
	ReadOnly  bool
}

// RawBytes is a byte slice that holds a reference to memory owned by
// the database itself. After a Scan into a RawBytes, the slice is only
// valid until the next call to Next, Scan, or Close.
type RawBytes []byte

// Scanner is an interface used by Scan.
type Scanner interface {
	// Scan assigns a value from a database driver.
	//
	// The src value will be of one of the following types:
	//
	//    int64
	//    float64
	//    bool
	//    []byte
	//    string
	//    time.Time
	//    nil - for NULL values
	//
	// An error should be returned if the value cannot be stored
	// without loss of information.
	//
	// Reference types such as []byte are only valid until the next call to Scan
	// and should not be retained. Their underlying memory is owned by the driver.
	// If retention is necessary, copy their values before the next call to Scan.
	Scan(src interface{}) error
}

// Out may be used to retrieve OUTPUT value parameters from stored procedures.
//
// Not all drivers and databases support OUTPUT value parameters.
//
// Example usage:
//
//   var outArg string
//   _, err := connPool.ExecContext(ctx, "ProcName", sql.Named("Arg1", sql.Out{Dest: &outArg}))
type Out struct {
	_Named_Fields_Required struct{}

	// Dest is a pointer to the value that will be set to the result of the
	// stored procedure's OUTPUT parameter.
	Dest interface{}

	// In is whether the parameter is an INOUT parameter. If so, the input value to the stored
	// procedure is the dereferenced value of Dest's pointer, which is then replaced with
	// the output value.
	In bool
}

// ErrNoRows is returned by Scan when QueryRow doesn't return a
// row. In such a case, QueryRow returns a placeholder *Row value that
// defers this error until a Scan.
var ErrNoRows = errors.New("sql: no rows in result set")

// connReuseStrategy determines how (*ConnPool).conn returns database connections.
type connReuseStrategy uint8

const (
	// alwaysNewConn forces a new connection to the database.
	alwaysNewConn connReuseStrategy = iota
	// cachedOrNewConn returns a cached connection, if available, else waits
	// for one to become available (if MaxOpenConns has been reached) or
	// creates a new database connection.
	cachedOrNewConn
)

// driverStmt associates a driver.Stmt with the
// *driverConn from which it came, so the driverConn's lock can be
// held during calls.
type driverStmt struct {
	sync.Locker // the *driverConn
	si          driver.Stmt
	closed      bool
	closeErr    error // return value of previous Close call
}

// Close ensures driver.Stmt is only closed once and always returns the same
// result.
func (ds *driverStmt) Close() error {
	ds.Lock()
	defer ds.Unlock()
	if ds.closed {
		return ds.closeErr
	}
	ds.closed = true
	ds.closeErr = ds.si.Close()
	return ds.closeErr
}

// depSet is a finalCloser's outstanding dependencies
type depSet map[interface{}]bool // set of true bools

// The finalCloser interface is used by (*ConnPool).addDep and related
// dependency reference counting.
type finalCloser interface {
	// finalClose is called when the reference count of an object
	// goes to zero. (*ConnPool).mu is not held while calling it.
	finalClose() error
}

// addDep notes that x now depends on dep, and x's finalClose won't be
// called until all of x's dependencies are removed with removeDep.
func (connPool *ConnPool) addDep(x finalCloser, dep interface{}) {
	connPool.mu.Lock()
	defer connPool.mu.Unlock()
	connPool.addDepLocked(x, dep)
}

func (connPool *ConnPool) addDepLocked(x finalCloser, dep interface{}) {
	if connPool.dep == nil {
		connPool.dep = make(map[finalCloser]depSet)
	}
	xdep := connPool.dep[x]
	if xdep == nil {
		xdep = make(depSet)
		connPool.dep[x] = xdep
	}
	xdep[dep] = true
}

// removeDep notes that x no longer depends on dep.
// If x still has dependencies, nil is returned.
// If x no longer has any dependencies, its finalClose method will be
// called and its error value will be returned.
func (connPool *ConnPool) removeDep(x finalCloser, dep interface{}) error {
	connPool.mu.Lock()
	fn := connPool.removeDepLocked(x, dep)
	connPool.mu.Unlock()
	return fn()
}

func (connPool *ConnPool) removeDepLocked(x finalCloser, dep interface{}) func() error {

	xdep, ok := connPool.dep[x]
	if !ok {
		panic(fmt.Sprintf("unpaired removeDep: no deps for %T", x))
	}

	l0 := len(xdep)
	delete(xdep, dep)

	switch len(xdep) {
	case l0:
		// Nothing removed. Shouldn't happen.
		panic(fmt.Sprintf("unpaired removeDep: no %T dep on %T", dep, x))
	case 0:
		// No more dependencies.
		delete(connPool.dep, x)
		return x.finalClose
	default:
		// Dependencies remain.
		return func() error { return nil }
	}
}

// This is the size of the connectionOpener request chan (ConnPool.openerCh).
// This value should be larger than the maximum typical value
// used for connPool.maxOpen. If maxOpen is significantly larger than
// connectionRequestQueueSize then it is possible for ALL calls into the *ConnPool
// to block until the connectionOpener can satisfy the backlog of requests.
var connectionRequestQueueSize = 1000000

// OpenDB opens a database using a Connector, allowing drivers to
// bypass a string based data source name.
//
// Most users will open a database via a driver-specific connection
// helper function that returns a *ConnPool. No database drivers are included
// in the Go standard library. See https://golang.org/s/sqldrivers for
// a list of third-party drivers.
//
// OpenDB may just validate its arguments without creating a connection
// to the database. To verify that the data source name is valid, call
// Ping.
//
// The returned ConnPool is safe for concurrent use by multiple goroutines
// and maintains its own pool of idle connections. Thus, the OpenDB
// function should be called just once. It is rarely necessary to
// close a ConnPool.
func OpenDB(c driver.Connector) *ConnPool {
	//ctx, cancel := context.WithCancel(context.Background())
	_, cancel := context.WithCancel(context.Background())
	connPool := &ConnPool{
		connector: c,
		openerCh:  make(chan struct{}, connectionRequestQueueSize),
		lastPut:   make(map[*driverConn]string),
		//TODO добавить установку менеджера пулов
		//connRequests: make(map[uint64]chan connCreationResponse),
		stop: cancel,
	}

	//TODO я заменил на globalConnectionOpener
	//go connPool.connectionOpener(ctx)

	return connPool
}

//TODO это нужно закрыть
// Open opens a database specified by its database driver name and a
// driver-specific data source name, usually consisting of at least a
// database name and connection information.
//
// Most users will open a database via a driver-specific connection
// helper function that returns a *ConnPool. No database drivers are included
// in the Go standard library. See https://golang.org/s/sqldrivers for
// a list of third-party drivers.
//
// Open may just validate its arguments without creating a connection
// to the database. To verify that the data source name is valid, call
// Ping.
//
// The returned ConnPool is safe for concurrent use by multiple goroutines
// and maintains its own pool of idle connections. Thus, the Open
// function should be called just once. It is rarely necessary to
// close a ConnPool.
func Open(driverName, dataSourceName string) (*ConnPool, error) {
	driversMu.RLock()
	driveri, ok := drivers[driverName]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sql: unknown driver %q (forgotten import?)", driverName)
	}

	if driverCtx, ok := driveri.(driver.DriverContext); ok {
		connector, err := driverCtx.OpenConnector(dataSourceName)
		if err != nil {
			return nil, err
		}
		return OpenDB(connector), nil
	}

	return OpenDB(dsnConnector{dsn: dataSourceName, driver: driveri}), nil
}

//TODO больше не нужно
//// SetMaxIdleConns sets the maximum number of connections in the idle
//// connection pool.
////
//// If MaxOpenConns is greater than 0 but less than the new MaxIdleConns,
//// then the new MaxIdleConns will be reduced to match the MaxOpenConns limit.
////
//// If n <= 0, no idle connections are retained.
////
//// The default max idle connections is currently 2. This may change in
//// a future release.
//func (connPool *ConnPool) SetMaxIdleConns(n int) {
//	connPool.mu.Lock()
//	if n > 0 {
//		connPool.maxIdleCount = n
//	} else {
//		// No idle connections.
//		connPool.maxIdleCount = -1
//	}
//	// Make sure maxIdle doesn't exceed maxOpen
//	if connPool.maxOpen > 0 && connPool.maxIdleConnsLocked() > connPool.maxOpen {
//		connPool.maxIdleCount = connPool.maxOpen
//	}
//	var closing []*driverConn
//	idleCount := len(connPool.freeConn)
//	maxIdle := connPool.maxIdleConnsLocked()
//	if idleCount > maxIdle {
//		closing = connPool.freeConn[maxIdle:]
//		connPool.freeConn = connPool.freeConn[:maxIdle]
//	}
//	connPool.maxIdleClosed += int64(len(closing))
//	connPool.mu.Unlock()
//	for _, c := range closing {
//		c.Close()
//	}
//}

//Больше не нужно
//// SetMaxOpenConns sets the maximum number of open connections to the database.
////
//// If MaxIdleConns is greater than 0 and the new MaxOpenConns is less than
//// MaxIdleConns, then MaxIdleConns will be reduced to match the new
//// MaxOpenConns limit.
////
//// If n <= 0, then there is no limit on the number of open connections.
//// The default is 0 (unlimited).
//func (connPool *ConnPool) SetMaxOpenConns(n int) {
//	connPool.mu.Lock()
//	connPool.maxOpen = n
//	if n < 0 {
//		connPool.maxOpen = 0
//	}
//	syncMaxIdle := connPool.maxOpen > 0 && connPool.maxIdleConnsLocked() > connPool.maxOpen
//	connPool.mu.Unlock()
//	if syncMaxIdle {
//		connPool.SetMaxIdleConns(n)
//	}
//}

// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
//
// Expired connections may be closed lazily before reuse.
//
// If d <= 0, connections are not closed due to a connection's age.
func (connPool *ConnPool) SetConnMaxLifetime(d time.Duration) {
	if d < 0 {
		d = 0
	}
	connPool.mu.Lock()
	// Wake cleaner up when lifetime is shortened.
	if d > 0 && d < connPool.maxLifetime && connPool.cleanerCh != nil {
		select {
		case connPool.cleanerCh <- struct{}{}:
		default:
		}
	}
	connPool.maxLifetime = d
	connPool.startCleanerLocked()
	connPool.mu.Unlock()
}

// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle.
//
// Expired connections may be closed lazily before reuse.
//
// If d <= 0, connections are not closed due to a connection's idle time.
func (connPool *ConnPool) SetConnMaxIdleTime(d time.Duration) {
	if d < 0 {
		d = 0
	}
	connPool.mu.Lock()
	defer connPool.mu.Unlock()

	// Wake cleaner up when idle time is shortened.
	if d > 0 && d < connPool.maxIdleTime && connPool.cleanerCh != nil {
		select {
		case connPool.cleanerCh <- struct{}{}:
		default:
		}
	}
	connPool.maxIdleTime = d
	connPool.startCleanerLocked()
}

// TODO заменил глобальным maybeOpenNewConnectionsLocked в poolFacade
// TODO просит создать запрошеное кол-во соединений у другой горутины
// TODO откуда вызывается?
// Assumes connPool.mu is locked.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
//func (connPool *ConnPool) maybeOpenNewConnectionsLocked() {
//	numRequests := len(connPool.connRequests)
//	if connPool.maxOpen > 0 {
//		numCanOpen := connPool.maxOpen - connPool.numOpen
//		if numCanOpen < numRequests {
//			numRequests = numCanOpen
//		}
//	}
//	for numRequests > 0 {
//		connPool.numOpen++ // optimistically
//		numRequests--
//		if connPool.closed {
//			return
//		}
//		connPool.openerCh <- struct{}{}
//	}
//}

//TODO я заменил на globalConnectionOpener
//// Runs in a separate goroutine, opens new connections when requested.
//// TODO изучить/ запускается параллелльно с созданием нового пула
//func (connPool *ConnPool) connectionOpener(ctx context.Context) {
//	//ждём пока контекст закроется или попросят открыть новое соединение
//	for {
//		select {
//		case <-ctx.Done():
//			return
//		case <-connPool.openerCh:
//			connPool.openNewConnection(ctx)
//		}
//	}
//}

// connCreationResponse represents one request for a new connection
// When there are no idle connections available, ConnPool.conn will create
// a new connCreationResponse and put it on the connPool.connRequests list.
type connCreationResponse struct {
	conn *driverConn
	err  error
}

var errDBClosed = errors.New("sql: database is closed")

type releaseConn func(error)

var rollbackHook func()

func rowsColumnInfoSetupConnLocked(rowsi driver.Rows) []*ColumnType {
	names := rowsi.Columns()

	list := make([]*ColumnType, len(names))
	for i := range list {
		ci := &ColumnType{
			name: names[i],
		}
		list[i] = ci

		if prop, ok := rowsi.(driver.RowsColumnTypeScanType); ok {
			ci.scanType = prop.ColumnTypeScanType(i)
		} else {
			ci.scanType = reflect.TypeOf(new(interface{})).Elem()
		}
		if prop, ok := rowsi.(driver.RowsColumnTypeDatabaseTypeName); ok {
			ci.databaseType = prop.ColumnTypeDatabaseTypeName(i)
		}
		if prop, ok := rowsi.(driver.RowsColumnTypeLength); ok {
			ci.length, ci.hasLength = prop.ColumnTypeLength(i)
		}
		if prop, ok := rowsi.(driver.RowsColumnTypeNullable); ok {
			ci.nullable, ci.hasNullable = prop.ColumnTypeNullable(i)
		}
		if prop, ok := rowsi.(driver.RowsColumnTypePrecisionScale); ok {
			ci.precision, ci.scale, ci.hasPrecisionScale = prop.ColumnTypePrecisionScale(i)
		}
	}
	return list
}

func stack() string {
	var buf [2 << 10]byte
	return string(buf[:runtime.Stack(buf[:], false)])
}

// withLock runs while holding lk.
func withLock(lk sync.Locker, fn func()) {
	lk.Lock()
	defer lk.Unlock() // in case fn panics
	fn()
}
