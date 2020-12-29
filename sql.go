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
package memsql_conn_pool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"memsql-conn-pool/driver"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
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

// NullString represents a string that may be null.
// NullString implements the Scanner interface so
// it can be used as a scan destination:
//
//  var s NullString
//  err := connPool.QueryRow("SELECT name FROM foo WHERE id=?", id).Scan(&s)
//  ...
//  if s.Valid {
//     // use s.String
//  } else {
//     // NULL value
//  }
//
type NullString struct {
	String string
	Valid  bool // Valid is true if String is not NULL
}

// Scan implements the Scanner interface.
func (ns *NullString) Scan(value interface{}) error {
	if value == nil {
		ns.String, ns.Valid = "", false
		return nil
	}
	ns.Valid = true
	return convertAssign(&ns.String, value)
}

// Value implements the driver Valuer interface.
func (ns NullString) Value() (driver.Value, error) {
	if !ns.Valid {
		return nil, nil
	}
	return ns.String, nil
}

// NullInt64 represents an int64 that may be null.
// NullInt64 implements the Scanner interface so
// it can be used as a scan destination, similar to NullString.
type NullInt64 struct {
	Int64 int64
	Valid bool // Valid is true if Int64 is not NULL
}

// Scan implements the Scanner interface.
func (n *NullInt64) Scan(value interface{}) error {
	if value == nil {
		n.Int64, n.Valid = 0, false
		return nil
	}
	n.Valid = true
	return convertAssign(&n.Int64, value)
}

// Value implements the driver Valuer interface.
func (n NullInt64) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Int64, nil
}

// NullInt32 represents an int32 that may be null.
// NullInt32 implements the Scanner interface so
// it can be used as a scan destination, similar to NullString.
type NullInt32 struct {
	Int32 int32
	Valid bool // Valid is true if Int32 is not NULL
}

// Scan implements the Scanner interface.
func (n *NullInt32) Scan(value interface{}) error {
	if value == nil {
		n.Int32, n.Valid = 0, false
		return nil
	}
	n.Valid = true
	return convertAssign(&n.Int32, value)
}

// Value implements the driver Valuer interface.
func (n NullInt32) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return int64(n.Int32), nil
}

// NullFloat64 represents a float64 that may be null.
// NullFloat64 implements the Scanner interface so
// it can be used as a scan destination, similar to NullString.
type NullFloat64 struct {
	Float64 float64
	Valid   bool // Valid is true if Float64 is not NULL
}

// Scan implements the Scanner interface.
func (n *NullFloat64) Scan(value interface{}) error {
	if value == nil {
		n.Float64, n.Valid = 0, false
		return nil
	}
	n.Valid = true
	return convertAssign(&n.Float64, value)
}

// Value implements the driver Valuer interface.
func (n NullFloat64) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Float64, nil
}

// NullBool represents a bool that may be null.
// NullBool implements the Scanner interface so
// it can be used as a scan destination, similar to NullString.
type NullBool struct {
	Bool  bool
	Valid bool // Valid is true if Bool is not NULL
}

// Scan implements the Scanner interface.
func (n *NullBool) Scan(value interface{}) error {
	if value == nil {
		n.Bool, n.Valid = false, false
		return nil
	}
	n.Valid = true
	return convertAssign(&n.Bool, value)
}

// Value implements the driver Valuer interface.
func (n NullBool) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Bool, nil
}

// NullTime represents a time.Time that may be null.
// NullTime implements the Scanner interface so
// it can be used as a scan destination, similar to NullString.
type NullTime struct {
	Time  time.Time
	Valid bool // Valid is true if Time is not NULL
}

// Scan implements the Scanner interface.
func (n *NullTime) Scan(value interface{}) error {
	if value == nil {
		n.Time, n.Valid = time.Time{}, false
		return nil
	}
	n.Valid = true
	return convertAssign(&n.Time, value)
}

// Value implements the driver Valuer interface.
func (n NullTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Time, nil
}

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

// driverConn wraps a driver.Conn with a mutex, to
// be held during all calls into the Conn. (including any calls onto
// interfaces returned via that Conn, such as calls on Tx, Stmt,
// Result, Rows)
type driverConn struct {
	connPool  *ConnPool
	createdAt time.Time

	sync.Mutex  // guards following
	ci          driver.Conn
	needReset   bool // The connection session should be reset before use if true.
	closed      bool
	finalClosed bool // ci.Close has been called
	openStmt    map[*driverStmt]bool

	// guarded by connPool.mu
	inUse      bool
	returnedAt time.Time // Time the connection was created or returned.
	onPut      []func()  // code (with connPool.mu held) run when conn is next returned
	dbmuClosed bool      // same as closed, but guarded by connPool.mu, for removeClosedStmtLocked
}

func (dc *driverConn) releaseConn(err error) {
	dc.connPool.putConn(dc, err, true)
}

func (dc *driverConn) removeOpenStmt(ds *driverStmt) {
	dc.Lock()
	defer dc.Unlock()
	delete(dc.openStmt, ds)
}

func (dc *driverConn) expired(timeout time.Duration) bool {
	if timeout <= 0 {
		return false
	}
	return dc.createdAt.Add(timeout).Before(nowFunc())
}

// resetSession checks if the driver connection needs the
// session to be reset and if required, resets it.
func (dc *driverConn) resetSession(ctx context.Context) error {
	dc.Lock()
	defer dc.Unlock()

	if !dc.needReset {
		return nil
	}
	if cr, ok := dc.ci.(driver.SessionResetter); ok {
		return cr.ResetSession(ctx)
	}
	return nil
}

// validateConnection checks if the connection is valid and can
// still be used. It also marks the session for reset if required.
func (dc *driverConn) validateConnection(needsReset bool) bool {
	dc.Lock()
	defer dc.Unlock()

	if needsReset {
		dc.needReset = true
	}
	if cv, ok := dc.ci.(driver.Validator); ok {
		return cv.IsValid()
	}
	return true
}

// prepareLocked prepares the query on dc. When cg == nil the dc must keep track of
// the prepared statements in a pool.
func (dc *driverConn) prepareLocked(ctx context.Context, cg stmtConnGrabber, query string) (*driverStmt, error) {
	si, err := ctxDriverPrepare(ctx, dc.ci, query)
	if err != nil {
		return nil, err
	}
	ds := &driverStmt{Locker: dc, si: si}

	// No need to manage open statements if there is a single connection grabber.
	if cg != nil {
		return ds, nil
	}

	// Track each driverConn's open statements, so we can close them
	// before closing the conn.
	//
	// Wrap all driver.Stmt is *driverStmt to ensure they are only closed once.
	if dc.openStmt == nil {
		dc.openStmt = make(map[*driverStmt]bool)
	}
	dc.openStmt[ds] = true
	return ds, nil
}

// the dc.connPool's Mutex is held.
func (dc *driverConn) closeDBLocked() func() error {
	dc.Lock()
	defer dc.Unlock()
	if dc.closed {
		return func() error { return errors.New("sql: duplicate driverConn close") }
	}
	dc.closed = true
	return dc.connPool.removeDepLocked(dc, dc)
}

func (dc *driverConn) Close() error {
	dc.Lock()
	if dc.closed {
		dc.Unlock()
		return errors.New("sql: duplicate driverConn close")
	}
	dc.closed = true
	dc.Unlock() // not defer; removeDep finalClose calls may need to lock

	// And now updates that require holding dc.mu.Lock.
	dc.connPool.mu.Lock()
	dc.dbmuClosed = true
	fn := dc.connPool.removeDepLocked(dc, dc)
	dc.connPool.mu.Unlock()
	return fn()
}

//TODO изучить / закрытие соединения
func (dc *driverConn) finalClose() error {
	var err error

	// Each *driverStmt has a lock to the dc. Copy the list out of the dc
	// before calling close on each stmt.
	var openStmt []*driverStmt
	withLock(dc, func() {
		openStmt = make([]*driverStmt, 0, len(dc.openStmt))
		for ds := range dc.openStmt {
			openStmt = append(openStmt, ds)
		}
		dc.openStmt = nil
	})
	for _, ds := range openStmt {
		ds.Close()
	}
	withLock(dc, func() {
		dc.finalClosed = true
		err = dc.ci.Close()
		dc.ci = nil
	})

	dc.connPool.mu.Lock()
	dc.connPool.numOpen--
	//TODO попытка открыть новые соединения
	log.Print("finalClose call maybeOpenNewConnectionsLocked")
	dc.connPool.poolManager.maybeOpenNewConnectionsLocked()
	dc.connPool.mu.Unlock()

	atomic.AddUint64(&dc.connPool.numClosed, 1)
	return err
}

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

type dsnConnector struct {
	dsn    string
	driver driver.Driver
}

func (t dsnConnector) Connect(_ context.Context) (driver.Conn, error) {
	return t.driver.Open(t.dsn)
}

func (t dsnConnector) Driver() driver.Driver {
	return t.driver
}

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

func (connPool *ConnPool) pingDC(ctx context.Context, dc *driverConn, release func(error)) error {
	var err error
	if pinger, ok := dc.ci.(driver.Pinger); ok {
		withLock(dc, func() {
			err = pinger.Ping(ctx)
		})
	}
	release(err)
	return err
}

// PingContext verifies a connection to the database is still alive,
// establishing a connection if necessary.
func (connPool *ConnPool) PingContext(ctx context.Context) error {
	var dc *driverConn
	var err error

	for i := 0; i < maxBadConnRetries; i++ {
		dc, err = connPool.conn(ctx, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		dc, err = connPool.conn(ctx, alwaysNewConn)
	}
	if err != nil {
		return err
	}

	return connPool.pingDC(ctx, dc, dc.releaseConn)
}

// Ping verifies a connection to the database is still alive,
// establishing a connection if necessary.
func (connPool *ConnPool) Ping() error {
	return connPool.PingContext(context.Background())
}

// Close closes the database and prevents new queries from starting.
// Close then waits for all queries that have started processing on the server
// to finish.
//
// It is rare to Close a ConnPool, as the ConnPool handle is meant to be
// long-lived and shared between many goroutines.
func (connPool *ConnPool) Close() error {
	connPool.mu.Lock()
	if connPool.closed { // Make ConnPool.Close idempotent
		connPool.mu.Unlock()
		return nil
	}
	if connPool.cleanerCh != nil {
		close(connPool.cleanerCh)
	}
	var err error
	fns := make([]func() error, 0, len(connPool.freeConn))
	for _, dc := range connPool.freeConn {
		fns = append(fns, dc.closeDBLocked())
	}
	connPool.freeConn = nil
	connPool.closed = true
	//TODO в пуле больше не содержатся запросы на создание новых соединений
	//for _, req := range connPool.connRequests {
	//	close(req)
	//}
	connPool.mu.Unlock()
	for _, fn := range fns {
		err1 := fn()
		if err1 != nil {
			err = err1
		}
	}
	connPool.stop()
	return err
}

const defaultMaxIdleConns = 2

func (connPool *ConnPool) maxIdleConnsLocked() int {
	n := connPool.maxIdleCount
	switch {
	case n == 0:
		// TODO(bradfitz): ask driver, if supported, for its default preference
		return defaultMaxIdleConns
	case n < 0:
		return 0
	default:
		return n
	}
}

func (connPool *ConnPool) shortestIdleTimeLocked() time.Duration {
	if connPool.maxIdleTime <= 0 {
		return connPool.maxLifetime
	}
	if connPool.maxLifetime <= 0 {
		return connPool.maxIdleTime
	}

	min := connPool.maxIdleTime
	if min > connPool.maxLifetime {
		min = connPool.maxLifetime
	}
	return min
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

// startCleanerLocked starts connectionCleaner if needed.
func (connPool *ConnPool) startCleanerLocked() {
	if (connPool.maxLifetime > 0 || connPool.maxIdleTime > 0) && connPool.numOpen > 0 && connPool.cleanerCh == nil {
		connPool.cleanerCh = make(chan struct{}, 1)
		go connPool.connectionCleaner(connPool.shortestIdleTimeLocked())
	}
}

func (connPool *ConnPool) connectionCleaner(d time.Duration) {
	const minInterval = time.Second

	if d < minInterval {
		d = minInterval
	}
	t := time.NewTimer(d)

	for {
		select {
		case <-t.C:
		case <-connPool.cleanerCh: // maxLifetime was changed or connPool was closed.
		}

		connPool.mu.Lock()

		d = connPool.shortestIdleTimeLocked()
		if connPool.closed || connPool.numOpen == 0 || d <= 0 {
			connPool.cleanerCh = nil
			connPool.mu.Unlock()
			return
		}

		closing := connPool.connectionCleanerRunLocked()
		connPool.mu.Unlock()
		for _, c := range closing {
			c.Close()
		}

		if d < minInterval {
			d = minInterval
		}
		t.Reset(d)
	}
}

func (connPool *ConnPool) connectionCleanerRunLocked() (closing []*driverConn) {
	if connPool.maxLifetime > 0 {
		expiredSince := nowFunc().Add(-connPool.maxLifetime)
		for i := 0; i < len(connPool.freeConn); i++ {
			c := connPool.freeConn[i]
			if c.createdAt.Before(expiredSince) {
				closing = append(closing, c)
				last := len(connPool.freeConn) - 1
				connPool.freeConn[i] = connPool.freeConn[last]
				connPool.freeConn[last] = nil
				connPool.freeConn = connPool.freeConn[:last]
				i--
			}
		}
		connPool.maxLifetimeClosed += int64(len(closing))
	}

	if connPool.maxIdleTime > 0 {
		expiredSince := nowFunc().Add(-connPool.maxIdleTime)
		var expiredCount int64
		for i := 0; i < len(connPool.freeConn); i++ {
			c := connPool.freeConn[i]
			if connPool.maxIdleTime > 0 && c.returnedAt.Before(expiredSince) {
				closing = append(closing, c)
				expiredCount++
				last := len(connPool.freeConn) - 1
				connPool.freeConn[i] = connPool.freeConn[last]
				connPool.freeConn[last] = nil
				connPool.freeConn = connPool.freeConn[:last]
				i--
			}
		}
		connPool.maxIdleTimeClosed += expiredCount
	}
	return
}

// DBStats contains database statistics.
type DBStats struct {
	//TODO unlimited
	//MaxOpenConnections int // Maximum number of open connections to the database.

	// Pool Status
	OpenConnections int // The number of established connections both in use and idle.
	InUse           int // The number of connections currently in use.
	Idle            int // The number of idle connections.

	// Counters
	WaitCount         int64         // The total number of connections waited for.
	WaitDuration      time.Duration // The total time blocked waiting for a new connection.
	MaxIdleClosed     int64         // The total number of connections closed due to SetMaxIdleConns.
	MaxIdleTimeClosed int64         // The total number of connections closed due to SetConnMaxIdleTime.
	MaxLifetimeClosed int64         // The total number of connections closed due to SetConnMaxLifetime.
}

// Stats returns database statistics.
func (connPool *ConnPool) Stats() DBStats {
	wait := atomic.LoadInt64(&connPool.waitDuration)

	connPool.mu.Lock()
	defer connPool.mu.Unlock()

	stats := DBStats{
		//MaxOpenConnections: connPool.maxOpen,

		Idle:            len(connPool.freeConn),
		OpenConnections: connPool.numOpen,
		InUse:           connPool.numOpen - len(connPool.freeConn),

		//WaitCount:         connPool.waitCount,
		WaitDuration:      time.Duration(wait),
		MaxIdleClosed:     connPool.maxIdleClosed,
		MaxIdleTimeClosed: connPool.maxIdleTimeClosed,
		MaxLifetimeClosed: connPool.maxLifetimeClosed,
	}
	return stats
}

// TODO заменил глобальным maybeOpenNewConnectionsLocked в PoolManager
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

// conn returns a newly-opened or cached *driverConn.
func (connPool *ConnPool) conn(ctx context.Context, strategy connReuseStrategy) (*driverConn, error) {
	// Check if the context is closed.
	connPool.mu.Lock()

	if connPool.closed {
		connPool.mu.Unlock()
		return nil, errDBClosed
	}

	// Check if the context is expired.
	select {
	default:
	case <-ctx.Done():
		connPool.mu.Unlock()
		return nil, ctx.Err()
	}
	lifetime := connPool.maxLifetime

	// Prefer a free connection, if possible.
	numFree := len(connPool.freeConn)
	canUseIdleConnection := strategy == cachedOrNewConn && numFree > 0
	if canUseIdleConnection {
		log.Print("can use idle connection")
		//remove idle connection from slice
		conn := connPool.freeConn[0]
		copy(connPool.freeConn, connPool.freeConn[1:])
		connPool.freeConn = connPool.freeConn[:numFree-1]

		//mark as isUse
		conn.inUse = true

		// Check if the driver connection is expired.
		if conn.expired(lifetime) {
			connPool.maxLifetimeClosed++
			connPool.mu.Unlock()
			conn.Close()
			return nil, driver.ErrBadConn
		}
		connPool.mu.Unlock()

		// Reset the session if required.
		if err := conn.resetSession(ctx); err == driver.ErrBadConn {
			conn.Close()
			return nil, driver.ErrBadConn
		}

		//Success. Found cached connection.
		return conn, nil
	}

	if connPool.poolManager.isOpenConnectionLimitExceeded() {
		log.Print("OpenConnectionLimitExceeded")
		// TODO try to remove idle connection from another connection pool and
		//  wait for free connection

		// Make the connCreationResponse channel. It's buffered so that the
		// connectionOpener doesn't block while waiting for the responseChan to be read.

		responseChan := make(chan connCreationResponse, 1)
		reqKey := connPool.poolManager.nextRequestKeyLocked()

		connPool.poolManager.mu.Lock()
		connPool.poolManager.connRequests[reqKey] = connRequest{
			connPool: connPool,
			responce: responseChan,
		}
		connPool.poolManager.waitCount++
		connPool.poolManager.mu.Unlock()
		connPool.mu.Unlock()

		waitStart := nowFunc()

		// Timeout the connection request with the context.
		select {
		//Context expired
		case <-ctx.Done():
			// Remove the connection request and ensure no value has been sent
			// on it after removing.
			connPool.poolManager.mu.Lock()
			delete(connPool.poolManager.connRequests, reqKey)
			connPool.poolManager.mu.Unlock()

			atomic.AddInt64(&connPool.waitDuration, int64(time.Since(waitStart)))

			select {
			default:
			//And new connection was created
			case ret, ok := <-responseChan:
				//put connection to free slice
				if ok && ret.conn != nil {
					connPool.putConn(ret.conn, ret.err, false)
				}
			}
			return nil, ctx.Err()
		case ret, ok := <-responseChan:
			atomic.AddInt64(&connPool.waitDuration, int64(time.Since(waitStart)))

			if !ok {
				return nil, errDBClosed
			}
			// Only check if the connection is expired if the strategy is cachedOrNewConns.
			// If we require a new connection, just re-use the connection without looking
			// at the expiry time. If it is expired, it will be checked when it is placed
			// back into the connection pool.
			// This prioritizes giving a valid connection to a client over the exact connection
			// lifetime, which could expire exactly after this point anyway.
			if strategy == cachedOrNewConn && ret.err == nil && ret.conn.expired(lifetime) {
				connPool.mu.Lock()
				connPool.maxLifetimeClosed++
				connPool.mu.Unlock()
				ret.conn.Close()
				return nil, driver.ErrBadConn
			}
			if ret.conn == nil {
				return nil, ret.err
			}

			// Reset the session if required.
			if err := ret.conn.resetSession(ctx); err == driver.ErrBadConn {
				ret.conn.Close()
				return nil, driver.ErrBadConn
			}
			return ret.conn, ret.err
		}
	}

	connPool.numOpen++ // optimistically
	connPool.mu.Unlock()
	ci, err := connPool.connector.Connect(ctx)
	if err != nil {
		connPool.mu.Lock()
		connPool.numOpen-- // correct for earlier optimism
		//TODO попытка открыть новые соедиения
		log.Print("conn call maybeOpenNewConnectionsLocked")
		connPool.poolManager.maybeOpenNewConnectionsLocked()
		connPool.mu.Unlock()
		return nil, err
	}
	connPool.mu.Lock()
	dc := &driverConn{
		connPool:   connPool,
		createdAt:  nowFunc(),
		returnedAt: nowFunc(),
		ci:         ci,
		inUse:      true,
	}
	connPool.addDepLocked(dc, dc)
	connPool.mu.Unlock()
	return dc, nil
}

// putConnHook is a hook for testing.
var putConnHook func(*ConnPool, *driverConn)

// noteUnusedDriverStatement notes that ds is no longer used and should
// be closed whenever possible (when c is next not in use), unless c is
// already closed.
func (connPool *ConnPool) noteUnusedDriverStatement(c *driverConn, ds *driverStmt) {
	connPool.mu.Lock()
	defer connPool.mu.Unlock()
	if c.inUse {
		c.onPut = append(c.onPut, func() {
			ds.Close()
		})
	} else {
		c.Lock()
		fc := c.finalClosed
		c.Unlock()
		if !fc {
			ds.Close()
		}
	}
}

// debugGetPut determines whether getConn & putConn calls' stack traces
// are returned for more verbose crashes.
const debugGetPut = false

// TODO изучить / кладёт соединение в пулл / вызывает создание новых соединений
// putConn adds a connection to the connPool's free pool.
// err is optionally the last error that occurred on this connection.
func (connPool *ConnPool) putConn(dc *driverConn, err error, resetSession bool) {
	if err != driver.ErrBadConn {
		if !dc.validateConnection(resetSession) {
			err = driver.ErrBadConn
		}
	}
	connPool.mu.Lock()
	if !dc.inUse {
		connPool.mu.Unlock()
		if debugGetPut {
			fmt.Printf("putConn(%v) DUPLICATE was: %s\n\nPREVIOUS was: %s", dc, stack(), connPool.lastPut[dc])
		}
		panic("sql: connection returned that was never out")
	}

	if err != driver.ErrBadConn && dc.expired(connPool.maxLifetime) {
		connPool.maxLifetimeClosed++
		err = driver.ErrBadConn
	}
	if debugGetPut {
		connPool.lastPut[dc] = stack()
	}
	dc.inUse = false
	dc.returnedAt = nowFunc()

	for _, fn := range dc.onPut {
		fn()
	}
	dc.onPut = nil

	if err == driver.ErrBadConn {
		// Don't reuse bad connections.
		// Since the conn is considered bad and is being discarded, treat it
		// as closed. Don't decrement the open count here, finalClose will
		// take care of that.
		log.Print("putConn call maybeOpenNewConnectionsLocked")
		connPool.poolManager.maybeOpenNewConnectionsLocked()
		connPool.mu.Unlock()
		dc.Close()
		return
	}
	if putConnHook != nil {
		putConnHook(connPool, dc)
	}
	added := connPool.putConnDBLocked(dc, nil)
	connPool.mu.Unlock()

	if !added {
		dc.Close()
		return
	}
}

// Satisfy a connCreationResponse or put the driverConn in the idle pool and return true
// or return false.
// putConnDBLocked will satisfy a connCreationResponse if there is one, or it will
// return the *driverConn to the freeConn list if err == nil and the idle
// connection limit will not be exceeded.
// If err != nil, the value of dc is ignored.
// If err == nil, then dc must not equal nil.
// If a connCreationResponse was fulfilled or the *driverConn was placed in the
// freeConn list, then true is returned, otherwise false is returned.
func (connPool *ConnPool) putConnDBLocked(dc *driverConn, err error) bool {
	if connPool.closed {
		return false
	}
	if connPool.poolManager.isOpenConnectionLimitExceeded() {
		return false
	}
	if c := len(connPool.poolManager.connRequests); c > 0 {
		var reqKey uint64
		var req chan connCreationResponse
		for key, value := range connPool.poolManager.connRequests {
			reqKey = key
			req = value.responce
			break
		}

		delete(connPool.poolManager.connRequests, reqKey) // Remove from pending requests.
		if err == nil {
			dc.inUse = true
		}
		//TODO моя проверка
		if req == nil {
			fmt.Println("request is null")
			return false
		}
		req <- connCreationResponse{
			conn: dc,
			err:  err,
		}
		return true
	} else if err == nil && !connPool.closed {
		if connPool.maxIdleConnsLocked() > len(connPool.freeConn) {
			connPool.freeConn = append(connPool.freeConn, dc)
			connPool.startCleanerLocked()
			return true
		}
		connPool.maxIdleClosed++
	}
	return false
}

// maxBadConnRetries is the number of maximum retries if the driver returns
// driver.ErrBadConn to signal a broken connection before forcing a new
// connection to be opened.
const maxBadConnRetries = 2

// PrepareContext creates a prepared statement for later queries or executions.
// Multiple queries or executions may be run concurrently from the
// returned statement.
// The caller must call the statement's Close method
// when the statement is no longer needed.
//
// The provided context is used for the preparation of the statement, not for the
// execution of the statement.
func (connPool *ConnPool) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	var stmt *Stmt
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		stmt, err = connPool.prepare(ctx, query, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		return connPool.prepare(ctx, query, alwaysNewConn)
	}
	return stmt, err
}

// Prepare creates a prepared statement for later queries or executions.
// Multiple queries or executions may be run concurrently from the
// returned statement.
// The caller must call the statement's Close method
// when the statement is no longer needed.
func (connPool *ConnPool) Prepare(query string) (*Stmt, error) {
	return connPool.PrepareContext(context.Background(), query)
}

func (connPool *ConnPool) prepare(ctx context.Context, query string, strategy connReuseStrategy) (*Stmt, error) {
	// TODO: check if connPool.driver supports an optional
	// driver.Preparer interface and call that instead, if so,
	// otherwise we make a prepared statement that's bound
	// to a connection, and to execute this prepared statement
	// we either need to use this connection (if it's free), else
	// get a new connection + re-prepare + execute on that one.
	dc, err := connPool.conn(ctx, strategy)
	if err != nil {
		return nil, err
	}
	return connPool.prepareDC(ctx, dc, dc.releaseConn, nil, query)
}

// prepareDC prepares a query on the driverConn and calls release before
// returning. When cg == nil it implies that a connection pool is used, and
// when cg != nil only a single driver connection is used.
func (connPool *ConnPool) prepareDC(ctx context.Context, dc *driverConn, release func(error), cg stmtConnGrabber, query string) (*Stmt, error) {
	var ds *driverStmt
	var err error
	defer func() {
		release(err)
	}()
	withLock(dc, func() {
		ds, err = dc.prepareLocked(ctx, cg, query)
	})
	if err != nil {
		return nil, err
	}
	stmt := &Stmt{
		db:    connPool,
		query: query,
		cg:    cg,
		cgds:  ds,
	}

	// When cg == nil this statement will need to keep track of various
	// connections they are prepared on and record the stmt dependency on
	// the ConnPool.
	if cg == nil {
		stmt.css = []connStmt{{dc, ds}}
		stmt.lastNumClosed = atomic.LoadUint64(&connPool.numClosed)
		connPool.addDep(stmt, stmt)
	}
	return stmt, nil
}

// ExecContext executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
func (connPool *ConnPool) ExecContext(ctx context.Context, query string, args ...interface{}) (Result, error) {
	var res Result
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		res, err = connPool.exec(ctx, query, args, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		return connPool.exec(ctx, query, args, alwaysNewConn)
	}
	return res, err
}

// Exec executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
func (connPool *ConnPool) Exec(query string, args ...interface{}) (Result, error) {
	return connPool.ExecContext(context.Background(), query, args...)
}

func (connPool *ConnPool) exec(ctx context.Context, query string, args []interface{}, strategy connReuseStrategy) (Result, error) {
	dc, err := connPool.conn(ctx, strategy)
	if err != nil {
		return nil, err
	}
	return connPool.execDC(ctx, dc, dc.releaseConn, query, args)
}

func (connPool *ConnPool) execDC(ctx context.Context, dc *driverConn, release func(error), query string, args []interface{}) (res Result, err error) {
	defer func() {
		release(err)
	}()
	execerCtx, ok := dc.ci.(driver.ExecerContext)
	var execer driver.Execer
	if !ok {
		execer, ok = dc.ci.(driver.Execer)
	}
	if ok {
		var nvdargs []driver.NamedValue
		var resi driver.Result
		withLock(dc, func() {
			nvdargs, err = driverArgsConnLocked(dc.ci, nil, args)
			if err != nil {
				return
			}
			resi, err = ctxDriverExec(ctx, execerCtx, execer, query, nvdargs)
		})
		if err != driver.ErrSkip {
			if err != nil {
				return nil, err
			}
			return driverResult{dc, resi}, nil
		}
	}

	var si driver.Stmt
	withLock(dc, func() {
		si, err = ctxDriverPrepare(ctx, dc.ci, query)
	})
	if err != nil {
		return nil, err
	}
	ds := &driverStmt{Locker: dc, si: si}
	defer ds.Close()
	return resultFromStatement(ctx, dc.ci, ds, args...)
}

// QueryContext executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
func (connPool *ConnPool) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	var rows *Rows
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		rows, err = connPool.query(ctx, query, args, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}

	if err == driver.ErrBadConn {
		return connPool.query(ctx, query, args, alwaysNewConn)
	}
	return rows, err
}

// Query executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
func (connPool *ConnPool) Query(query string, args ...interface{}) (*Rows, error) {
	return connPool.QueryContext(context.Background(), query, args...)
}

func (connPool *ConnPool) query(ctx context.Context, query string, args []interface{}, strategy connReuseStrategy) (*Rows, error) {
	dc, err := connPool.conn(ctx, strategy)
	if err != nil {
		return nil, err
	}

	return connPool.queryDC(ctx, nil, dc, dc.releaseConn, query, args)
}

// queryDC executes a query on the given connection.
// The connection gets released by the releaseConn function.
// The ctx context is from a query method and the txctx context is from an
// optional transaction context.
func (connPool *ConnPool) queryDC(ctx, txctx context.Context, dc *driverConn, releaseConn func(error), query string, args []interface{}) (*Rows, error) {
	queryerCtx, ok := dc.ci.(driver.QueryerContext)
	var queryer driver.Queryer
	if !ok {
		queryer, ok = dc.ci.(driver.Queryer)
	}
	if ok {
		var nvdargs []driver.NamedValue
		var rowsi driver.Rows
		var err error
		withLock(dc, func() {
			nvdargs, err = driverArgsConnLocked(dc.ci, nil, args)
			if err != nil {
				return
			}
			rowsi, err = ctxDriverQuery(ctx, queryerCtx, queryer, query, nvdargs)
		})
		if err != driver.ErrSkip {
			if err != nil {
				releaseConn(err)
				return nil, err
			}
			// Note: ownership of dc passes to the *Rows, to be freed
			// with releaseConn.
			rows := &Rows{
				dc:          dc,
				releaseConn: releaseConn,
				rowsi:       rowsi,
			}
			rows.initContextClose(ctx, txctx)
			return rows, nil
		}
	}

	var si driver.Stmt
	var err error
	withLock(dc, func() {
		si, err = ctxDriverPrepare(ctx, dc.ci, query)
	})
	if err != nil {
		releaseConn(err)
		return nil, err
	}

	ds := &driverStmt{Locker: dc, si: si}
	rowsi, err := rowsiFromStatement(ctx, dc.ci, ds, args...)
	if err != nil {
		ds.Close()
		releaseConn(err)
		return nil, err
	}

	// Note: ownership of ci passes to the *Rows, to be freed
	// with releaseConn.
	rows := &Rows{
		dc:          dc,
		releaseConn: releaseConn,
		rowsi:       rowsi,
		closeStmt:   ds,
	}
	rows.initContextClose(ctx, txctx)
	return rows, nil
}

// QueryRowContext executes a query that is expected to return at most one row.
// QueryRowContext always returns a non-nil value. Errors are deferred until
// Row's Scan method is called.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
func (connPool *ConnPool) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := connPool.QueryContext(ctx, query, args...)
	return &Row{rows: rows, err: err}
}

// QueryRow executes a query that is expected to return at most one row.
// QueryRow always returns a non-nil value. Errors are deferred until
// Row's Scan method is called.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
func (connPool *ConnPool) QueryRow(query string, args ...interface{}) *Row {
	return connPool.QueryRowContext(context.Background(), query, args...)
}

// BeginTx starts a transaction.
//
// The provided context is used until the transaction is committed or rolled back.
// If the context is canceled, the sql package will roll back
// the transaction. Tx.Commit will return an error if the context provided to
// BeginTx is canceled.
//
// The provided TxOptions is optional and may be nil if defaults should be used.
// If a non-default isolation level is used that the driver doesn't support,
// an error will be returned.
func (connPool *ConnPool) BeginTx(ctx context.Context, opts *TxOptions) (*Tx, error) {
	var tx *Tx
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		tx, err = connPool.begin(ctx, opts, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		return connPool.begin(ctx, opts, alwaysNewConn)
	}
	return tx, err
}

// Begin starts a transaction. The default isolation level is dependent on
// the driver.
func (connPool *ConnPool) Begin() (*Tx, error) {
	return connPool.BeginTx(context.Background(), nil)
}

func (connPool *ConnPool) begin(ctx context.Context, opts *TxOptions, strategy connReuseStrategy) (tx *Tx, err error) {
	dc, err := connPool.conn(ctx, strategy)
	if err != nil {
		return nil, err
	}
	return connPool.beginDC(ctx, dc, dc.releaseConn, opts)
}

// beginDC starts a transaction. The provided dc must be valid and ready to use.
func (connPool *ConnPool) beginDC(ctx context.Context, dc *driverConn, release func(error), opts *TxOptions) (tx *Tx, err error) {
	var txi driver.Tx
	keepConnOnRollback := false
	withLock(dc, func() {
		_, hasSessionResetter := dc.ci.(driver.SessionResetter)
		_, hasConnectionValidator := dc.ci.(driver.Validator)
		keepConnOnRollback = hasSessionResetter && hasConnectionValidator
		txi, err = ctxDriverBegin(ctx, opts, dc.ci)
	})
	if err != nil {
		release(err)
		return nil, err
	}

	// Schedule the transaction to rollback when the context is cancelled.
	// The cancel function in Tx will be called after done is set to true.
	ctx, cancel := context.WithCancel(ctx)
	tx = &Tx{
		db:                 connPool,
		dc:                 dc,
		releaseConn:        release,
		txi:                txi,
		cancel:             cancel,
		keepConnOnRollback: keepConnOnRollback,
		ctx:                ctx,
	}
	go tx.awaitDone()
	return tx, nil
}

// Driver returns the database's underlying driver.
func (connPool *ConnPool) Driver() driver.Driver {
	return connPool.connector.Driver()
}

// ErrConnDone is returned by any operation that is performed on a connection
// that has already been returned to the connection pool.
var ErrConnDone = errors.New("sql: connection is already closed")

// Conn returns a single connection by either opening a new connection
// or returning an existing connection from the connection pool. Conn will
// block until either a connection is returned or ctx is canceled.
// Queries run on the same Conn will be run in the same database session.
//
// Every Conn must be returned to the database pool after use by
// calling Conn.Close.
func (connPool *ConnPool) Conn(ctx context.Context) (*Conn, error) {
	var dc *driverConn
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		dc, err = connPool.conn(ctx, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		dc, err = connPool.conn(ctx, alwaysNewConn)
	}
	if err != nil {
		return nil, err
	}

	conn := &Conn{
		db: connPool,
		dc: dc,
	}
	return conn, nil
}

type releaseConn func(error)

// Conn represents a single database connection rather than a pool of database
// connections. Prefer running queries from ConnPool unless there is a specific
// need for a continuous single database connection.
//
// A Conn must call Close to return the connection to the database pool
// and may do so concurrently with a running query.
//
// After a call to Close, all operations on the
// connection fail with ErrConnDone.
type Conn struct {
	db *ConnPool

	// closemu prevents the connection from closing while there
	// is an active query. It is held for read during queries
	// and exclusively during close.
	closemu sync.RWMutex

	// dc is owned until close, at which point
	// it's returned to the connection pool.
	dc *driverConn

	// done transitions from 0 to 1 exactly once, on close.
	// Once done, all operations fail with ErrConnDone.
	// Use atomic operations on value when checking value.
	done int32
}

// grabConn takes a context to implement stmtConnGrabber
// but the context is not used.
func (c *Conn) grabConn(context.Context) (*driverConn, releaseConn, error) {
	if atomic.LoadInt32(&c.done) != 0 {
		return nil, nil, ErrConnDone
	}
	c.closemu.RLock()
	return c.dc, c.closemuRUnlockCondReleaseConn, nil
}

// PingContext verifies the connection to the database is still alive.
func (c *Conn) PingContext(ctx context.Context) error {
	dc, release, err := c.grabConn(ctx)
	if err != nil {
		return err
	}
	return c.db.pingDC(ctx, dc, release)
}

// ExecContext executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
func (c *Conn) ExecContext(ctx context.Context, query string, args ...interface{}) (Result, error) {
	dc, release, err := c.grabConn(ctx)
	if err != nil {
		return nil, err
	}
	return c.db.execDC(ctx, dc, release, query, args)
}

// QueryContext executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
func (c *Conn) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	dc, release, err := c.grabConn(ctx)
	if err != nil {
		return nil, err
	}
	return c.db.queryDC(ctx, nil, dc, release, query, args)
}

// QueryRowContext executes a query that is expected to return at most one row.
// QueryRowContext always returns a non-nil value. Errors are deferred until
// Row's Scan method is called.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
func (c *Conn) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := c.QueryContext(ctx, query, args...)
	return &Row{rows: rows, err: err}
}

// PrepareContext creates a prepared statement for later queries or executions.
// Multiple queries or executions may be run concurrently from the
// returned statement.
// The caller must call the statement's Close method
// when the statement is no longer needed.
//
// The provided context is used for the preparation of the statement, not for the
// execution of the statement.
func (c *Conn) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	dc, release, err := c.grabConn(ctx)
	if err != nil {
		return nil, err
	}
	return c.db.prepareDC(ctx, dc, release, c, query)
}

// Raw executes f exposing the underlying driver connection for the
// duration of f. The driverConn must not be used outside of f.
//
// Once f returns and err is nil, the Conn will continue to be usable
// until Conn.Close is called.
func (c *Conn) Raw(f func(driverConn interface{}) error) (err error) {
	var dc *driverConn
	var release releaseConn

	// grabConn takes a context to implement stmtConnGrabber, but the context is not used.
	dc, release, err = c.grabConn(nil)
	if err != nil {
		return
	}
	fPanic := true
	dc.Mutex.Lock()
	defer func() {
		dc.Mutex.Unlock()

		// If f panics fPanic will remain true.
		// Ensure an error is passed to release so the connection
		// may be discarded.
		if fPanic {
			err = driver.ErrBadConn
		}
		release(err)
	}()
	err = f(dc.ci)
	fPanic = false

	return
}

// BeginTx starts a transaction.
//
// The provided context is used until the transaction is committed or rolled back.
// If the context is canceled, the sql package will roll back
// the transaction. Tx.Commit will return an error if the context provided to
// BeginTx is canceled.
//
// The provided TxOptions is optional and may be nil if defaults should be used.
// If a non-default isolation level is used that the driver doesn't support,
// an error will be returned.
func (c *Conn) BeginTx(ctx context.Context, opts *TxOptions) (*Tx, error) {
	dc, release, err := c.grabConn(ctx)
	if err != nil {
		return nil, err
	}
	return c.db.beginDC(ctx, dc, release, opts)
}

// closemuRUnlockCondReleaseConn read unlocks closemu
// as the sql operation is done with the dc.
func (c *Conn) closemuRUnlockCondReleaseConn(err error) {
	c.closemu.RUnlock()
	if err == driver.ErrBadConn {
		c.close(err)
	}
}

func (c *Conn) txCtx() context.Context {
	return nil
}

func (c *Conn) close(err error) error {
	if !atomic.CompareAndSwapInt32(&c.done, 0, 1) {
		return ErrConnDone
	}

	// Lock around releasing the driver connection
	// to ensure all queries have been stopped before doing so.
	c.closemu.Lock()
	defer c.closemu.Unlock()

	c.dc.releaseConn(err)
	c.dc = nil
	c.db = nil
	return err
}

// Close returns the connection to the connection pool.
// All operations after a Close will return with ErrConnDone.
// Close is safe to call concurrently with other operations and will
// block until all other operations finish. It may be useful to first
// cancel any used context and then call close directly after.
func (c *Conn) Close() error {
	return c.close(nil)
}

// Tx is an in-progress database transaction.
//
// A transaction must end with a call to Commit or Rollback.
//
// After a call to Commit or Rollback, all operations on the
// transaction fail with ErrTxDone.
//
// The statements prepared for a transaction by calling
// the transaction's Prepare or Stmt methods are closed
// by the call to Commit or Rollback.
type Tx struct {
	db *ConnPool

	// closemu prevents the transaction from closing while there
	// is an active query. It is held for read during queries
	// and exclusively during close.
	closemu sync.RWMutex

	// dc is owned exclusively until Commit or Rollback, at which point
	// it's returned with putConn.
	dc  *driverConn
	txi driver.Tx

	// releaseConn is called once the Tx is closed to release
	// any held driverConn back to the pool.
	releaseConn func(error)

	// done transitions from 0 to 1 exactly once, on Commit
	// or Rollback. once done, all operations fail with
	// ErrTxDone.
	// Use atomic operations on value when checking value.
	done int32

	// keepConnOnRollback is true if the driver knows
	// how to reset the connection's session and if need be discard
	// the connection.
	keepConnOnRollback bool

	// All Stmts prepared for this transaction. These will be closed after the
	// transaction has been committed or rolled back.
	stmts struct {
		sync.Mutex
		v []*Stmt
	}

	// cancel is called after done transitions from 0 to 1.
	cancel func()

	// ctx lives for the life of the transaction.
	ctx context.Context
}

// awaitDone blocks until the context in Tx is canceled and rolls back
// the transaction if it's not already done.
func (tx *Tx) awaitDone() {
	// Wait for either the transaction to be committed or rolled
	// back, or for the associated context to be closed.
	<-tx.ctx.Done()

	// Discard and close the connection used to ensure the
	// transaction is closed and the resources are released.  This
	// rollback does nothing if the transaction has already been
	// committed or rolled back.
	// Do not discard the connection if the connection knows
	// how to reset the session.
	discardConnection := !tx.keepConnOnRollback
	tx.rollback(discardConnection)
}

func (tx *Tx) isDone() bool {
	return atomic.LoadInt32(&tx.done) != 0
}

// ErrTxDone is returned by any operation that is performed on a transaction
// that has already been committed or rolled back.
var ErrTxDone = errors.New("sql: transaction has already been committed or rolled back")

// close returns the connection to the pool and
// must only be called by Tx.rollback or Tx.Commit while
// tx is already canceled and won't be executed concurrently.
func (tx *Tx) close(err error) {
	tx.releaseConn(err)
	tx.dc = nil
	tx.txi = nil
}

// hookTxGrabConn specifies an optional hook to be called on
// a successful call to (*Tx).grabConn. For tests.
var hookTxGrabConn func()

func (tx *Tx) grabConn(ctx context.Context) (*driverConn, releaseConn, error) {
	select {
	default:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	// closemu.RLock must come before the check for isDone to prevent the Tx from
	// closing while a query is executing.
	tx.closemu.RLock()
	if tx.isDone() {
		tx.closemu.RUnlock()
		return nil, nil, ErrTxDone
	}
	if hookTxGrabConn != nil { // test hook
		hookTxGrabConn()
	}
	return tx.dc, tx.closemuRUnlockRelease, nil
}

func (tx *Tx) txCtx() context.Context {
	return tx.ctx
}

// closemuRUnlockRelease is used as a func(error) method value in
// ExecContext and QueryContext. Unlocking in the releaseConn keeps
// the driver conn from being returned to the connection pool until
// the Rows has been closed.
func (tx *Tx) closemuRUnlockRelease(error) {
	tx.closemu.RUnlock()
}

// Closes all Stmts prepared for this transaction.
func (tx *Tx) closePrepared() {
	tx.stmts.Lock()
	defer tx.stmts.Unlock()
	for _, stmt := range tx.stmts.v {
		stmt.Close()
	}
}

// Commit commits the transaction.
func (tx *Tx) Commit() error {
	// Check context first to avoid transaction leak.
	// If put it behind tx.done CompareAndSwap statement, we can't ensure
	// the consistency between tx.done and the real COMMIT operation.
	select {
	default:
	case <-tx.ctx.Done():
		if atomic.LoadInt32(&tx.done) == 1 {
			return ErrTxDone
		}
		return tx.ctx.Err()
	}
	if !atomic.CompareAndSwapInt32(&tx.done, 0, 1) {
		return ErrTxDone
	}

	// Cancel the Tx to release any active R-closemu locks.
	// This is safe to do because tx.done has already transitioned
	// from 0 to 1. Hold the W-closemu lock prior to rollback
	// to ensure no other connection has an active query.
	tx.cancel()
	tx.closemu.Lock()
	tx.closemu.Unlock()

	var err error
	withLock(tx.dc, func() {
		err = tx.txi.Commit()
	})
	if err != driver.ErrBadConn {
		tx.closePrepared()
	}
	tx.close(err)
	return err
}

var rollbackHook func()

// rollback aborts the transaction and optionally forces the pool to discard
// the connection.
func (tx *Tx) rollback(discardConn bool) error {
	if !atomic.CompareAndSwapInt32(&tx.done, 0, 1) {
		return ErrTxDone
	}

	if rollbackHook != nil {
		rollbackHook()
	}

	// Cancel the Tx to release any active R-closemu locks.
	// This is safe to do because tx.done has already transitioned
	// from 0 to 1. Hold the W-closemu lock prior to rollback
	// to ensure no other connection has an active query.
	tx.cancel()
	tx.closemu.Lock()
	tx.closemu.Unlock()

	var err error
	withLock(tx.dc, func() {
		err = tx.txi.Rollback()
	})
	if err != driver.ErrBadConn {
		tx.closePrepared()
	}
	if discardConn {
		err = driver.ErrBadConn
	}
	tx.close(err)
	return err
}

// Rollback aborts the transaction.
func (tx *Tx) Rollback() error {
	return tx.rollback(false)
}

// PrepareContext creates a prepared statement for use within a transaction.
//
// The returned statement operates within the transaction and will be closed
// when the transaction has been committed or rolled back.
//
// To use an existing prepared statement on this transaction, see Tx.Stmt.
//
// The provided context will be used for the preparation of the context, not
// for the execution of the returned statement. The returned statement
// will run in the transaction context.
func (tx *Tx) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	dc, release, err := tx.grabConn(ctx)
	if err != nil {
		return nil, err
	}

	stmt, err := tx.db.prepareDC(ctx, dc, release, tx, query)
	if err != nil {
		return nil, err
	}
	tx.stmts.Lock()
	tx.stmts.v = append(tx.stmts.v, stmt)
	tx.stmts.Unlock()
	return stmt, nil
}

// Prepare creates a prepared statement for use within a transaction.
//
// The returned statement operates within the transaction and can no longer
// be used once the transaction has been committed or rolled back.
//
// To use an existing prepared statement on this transaction, see Tx.Stmt.
func (tx *Tx) Prepare(query string) (*Stmt, error) {
	return tx.PrepareContext(context.Background(), query)
}

// StmtContext returns a transaction-specific prepared statement from
// an existing statement.
//
// Example:
//  updateMoney, err := connPool.Prepare("UPDATE balance SET money=money+? WHERE id=?")
//  ...
//  tx, err := connPool.Begin()
//  ...
//  res, err := tx.StmtContext(ctx, updateMoney).Exec(123.45, 98293203)
//
// The provided context is used for the preparation of the statement, not for the
// execution of the statement.
//
// The returned statement operates within the transaction and will be closed
// when the transaction has been committed or rolled back.
func (tx *Tx) StmtContext(ctx context.Context, stmt *Stmt) *Stmt {
	dc, release, err := tx.grabConn(ctx)
	if err != nil {
		return &Stmt{stickyErr: err}
	}
	defer release(nil)

	if tx.db != stmt.db {
		return &Stmt{stickyErr: errors.New("sql: Tx.Stmt: statement from different database used")}
	}
	var si driver.Stmt
	var parentStmt *Stmt
	stmt.mu.Lock()
	if stmt.closed || stmt.cg != nil {
		// If the statement has been closed or already belongs to a
		// transaction, we can't reuse it in this connection.
		// Since tx.StmtContext should never need to be called with a
		// Stmt already belonging to tx, we ignore this edge case and
		// re-prepare the statement in this case. No need to add
		// code-complexity for this.
		stmt.mu.Unlock()
		withLock(dc, func() {
			si, err = ctxDriverPrepare(ctx, dc.ci, stmt.query)
		})
		if err != nil {
			return &Stmt{stickyErr: err}
		}
	} else {
		stmt.removeClosedStmtLocked()
		// See if the statement has already been prepared on this connection,
		// and reuse it if possible.
		for _, v := range stmt.css {
			if v.dc == dc {
				si = v.ds.si
				break
			}
		}

		stmt.mu.Unlock()

		if si == nil {
			var ds *driverStmt
			withLock(dc, func() {
				ds, err = stmt.prepareOnConnLocked(ctx, dc)
			})
			if err != nil {
				return &Stmt{stickyErr: err}
			}
			si = ds.si
		}
		parentStmt = stmt
	}

	txs := &Stmt{
		db: tx.db,
		cg: tx,
		cgds: &driverStmt{
			Locker: dc,
			si:     si,
		},
		parentStmt: parentStmt,
		query:      stmt.query,
	}
	if parentStmt != nil {
		tx.db.addDep(parentStmt, txs)
	}
	tx.stmts.Lock()
	tx.stmts.v = append(tx.stmts.v, txs)
	tx.stmts.Unlock()
	return txs
}

// Stmt returns a transaction-specific prepared statement from
// an existing statement.
//
// Example:
//  updateMoney, err := connPool.Prepare("UPDATE balance SET money=money+? WHERE id=?")
//  ...
//  tx, err := connPool.Begin()
//  ...
//  res, err := tx.Stmt(updateMoney).Exec(123.45, 98293203)
//
// The returned statement operates within the transaction and will be closed
// when the transaction has been committed or rolled back.
func (tx *Tx) Stmt(stmt *Stmt) *Stmt {
	return tx.StmtContext(context.Background(), stmt)
}

// ExecContext executes a query that doesn't return rows.
// For example: an INSERT and UPDATE.
func (tx *Tx) ExecContext(ctx context.Context, query string, args ...interface{}) (Result, error) {
	dc, release, err := tx.grabConn(ctx)
	if err != nil {
		return nil, err
	}
	return tx.db.execDC(ctx, dc, release, query, args)
}

// Exec executes a query that doesn't return rows.
// For example: an INSERT and UPDATE.
func (tx *Tx) Exec(query string, args ...interface{}) (Result, error) {
	return tx.ExecContext(context.Background(), query, args...)
}

// QueryContext executes a query that returns rows, typically a SELECT.
func (tx *Tx) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	dc, release, err := tx.grabConn(ctx)
	if err != nil {
		return nil, err
	}

	return tx.db.queryDC(ctx, tx.ctx, dc, release, query, args)
}

// Query executes a query that returns rows, typically a SELECT.
func (tx *Tx) Query(query string, args ...interface{}) (*Rows, error) {
	return tx.QueryContext(context.Background(), query, args...)
}

// QueryRowContext executes a query that is expected to return at most one row.
// QueryRowContext always returns a non-nil value. Errors are deferred until
// Row's Scan method is called.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := tx.QueryContext(ctx, query, args...)
	return &Row{rows: rows, err: err}
}

// QueryRow executes a query that is expected to return at most one row.
// QueryRow always returns a non-nil value. Errors are deferred until
// Row's Scan method is called.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
func (tx *Tx) QueryRow(query string, args ...interface{}) *Row {
	return tx.QueryRowContext(context.Background(), query, args...)
}

// connStmt is a prepared statement on a particular connection.
type connStmt struct {
	dc *driverConn
	ds *driverStmt
}

// stmtConnGrabber represents a Tx or Conn that will return the underlying
// driverConn and release function.
type stmtConnGrabber interface {
	// grabConn returns the driverConn and the associated release function
	// that must be called when the operation completes.
	grabConn(context.Context) (*driverConn, releaseConn, error)

	// txCtx returns the transaction context if available.
	// The returned context should be selected on along with
	// any query context when awaiting a cancel.
	txCtx() context.Context
}

var (
	_ stmtConnGrabber = &Tx{}
	_ stmtConnGrabber = &Conn{}
)

// Stmt is a prepared statement.
// A Stmt is safe for concurrent use by multiple goroutines.
//
// If a Stmt is prepared on a Tx or Conn, it will be bound to a single
// underlying connection forever. If the Tx or Conn closes, the Stmt will
// become unusable and all operations will return an error.
// If a Stmt is prepared on a ConnPool, it will remain usable for the lifetime of the
// ConnPool. When the Stmt needs to execute on a new underlying connection, it will
// prepare itself on the new connection automatically.
type Stmt struct {
	// Immutable:
	db        *ConnPool // where we came from
	query     string    // that created the Stmt
	stickyErr error     // if non-nil, this error is returned for all operations

	closemu sync.RWMutex // held exclusively during close, for read otherwise.

	// If Stmt is prepared on a Tx or Conn then cg is present and will
	// only ever grab a connection from cg.
	// If cg is nil then the Stmt must grab an arbitrary connection
	// from db and determine if it must prepare the stmt again by
	// inspecting css.
	cg   stmtConnGrabber
	cgds *driverStmt

	// parentStmt is set when a transaction-specific statement
	// is requested from an identical statement prepared on the same
	// conn. parentStmt is used to track the dependency of this statement
	// on its originating ("parent") statement so that parentStmt may
	// be closed by the user without them having to know whether or not
	// any transactions are still using it.
	parentStmt *Stmt

	mu     sync.Mutex // protects the rest of the fields
	closed bool

	// css is a list of underlying driver statement interfaces
	// that are valid on particular connections. This is only
	// used if cg == nil and one is found that has idle
	// connections. If cg != nil, cgds is always used.
	css []connStmt

	// lastNumClosed is copied from connPool.numClosed when Stmt is created
	// without tx and closed connections in css are removed.
	lastNumClosed uint64
}

// ExecContext executes a prepared statement with the given arguments and
// returns a Result summarizing the effect of the statement.
func (s *Stmt) ExecContext(ctx context.Context, args ...interface{}) (Result, error) {
	s.closemu.RLock()
	defer s.closemu.RUnlock()

	var res Result
	strategy := cachedOrNewConn
	for i := 0; i < maxBadConnRetries+1; i++ {
		if i == maxBadConnRetries {
			strategy = alwaysNewConn
		}
		dc, releaseConn, ds, err := s.connStmt(ctx, strategy)
		if err != nil {
			if err == driver.ErrBadConn {
				continue
			}
			return nil, err
		}

		res, err = resultFromStatement(ctx, dc.ci, ds, args...)
		releaseConn(err)
		if err != driver.ErrBadConn {
			return res, err
		}
	}
	return nil, driver.ErrBadConn
}

// Exec executes a prepared statement with the given arguments and
// returns a Result summarizing the effect of the statement.
func (s *Stmt) Exec(args ...interface{}) (Result, error) {
	return s.ExecContext(context.Background(), args...)
}

func resultFromStatement(ctx context.Context, ci driver.Conn, ds *driverStmt, args ...interface{}) (Result, error) {
	ds.Lock()
	defer ds.Unlock()

	dargs, err := driverArgsConnLocked(ci, ds, args)
	if err != nil {
		return nil, err
	}

	resi, err := ctxDriverStmtExec(ctx, ds.si, dargs)
	if err != nil {
		return nil, err
	}
	return driverResult{ds.Locker, resi}, nil
}

// removeClosedStmtLocked removes closed conns in s.css.
//
// To avoid lock contention on ConnPool.mu, we do it only when
// s.connPool.numClosed - s.lastNum is large enough.
func (s *Stmt) removeClosedStmtLocked() {
	t := len(s.css)/2 + 1
	if t > 10 {
		t = 10
	}
	dbClosed := atomic.LoadUint64(&s.db.numClosed)
	if dbClosed-s.lastNumClosed < uint64(t) {
		return
	}

	s.db.mu.Lock()
	for i := 0; i < len(s.css); i++ {
		if s.css[i].dc.dbmuClosed {
			s.css[i] = s.css[len(s.css)-1]
			s.css = s.css[:len(s.css)-1]
			i--
		}
	}
	s.db.mu.Unlock()
	s.lastNumClosed = dbClosed
}

// connStmt returns a free driver connection on which to execute the
// statement, a function to call to release the connection, and a
// statement bound to that connection.
func (s *Stmt) connStmt(ctx context.Context, strategy connReuseStrategy) (dc *driverConn, releaseConn func(error), ds *driverStmt, err error) {
	if err = s.stickyErr; err != nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		err = errors.New("sql: statement is closed")
		return
	}

	// In a transaction or connection, we always use the connection that the
	// stmt was created on.
	if s.cg != nil {
		s.mu.Unlock()
		dc, releaseConn, err = s.cg.grabConn(ctx) // blocks, waiting for the connection.
		if err != nil {
			return
		}
		return dc, releaseConn, s.cgds, nil
	}

	s.removeClosedStmtLocked()
	s.mu.Unlock()

	dc, err = s.db.conn(ctx, strategy)
	if err != nil {
		return nil, nil, nil, err
	}

	s.mu.Lock()
	for _, v := range s.css {
		if v.dc == dc {
			s.mu.Unlock()
			return dc, dc.releaseConn, v.ds, nil
		}
	}
	s.mu.Unlock()

	// No luck; we need to prepare the statement on this connection
	withLock(dc, func() {
		ds, err = s.prepareOnConnLocked(ctx, dc)
	})
	if err != nil {
		dc.releaseConn(err)
		return nil, nil, nil, err
	}

	return dc, dc.releaseConn, ds, nil
}

// prepareOnConnLocked prepares the query in Stmt s on dc and adds it to the list of
// open connStmt on the statement. It assumes the caller is holding the lock on dc.
func (s *Stmt) prepareOnConnLocked(ctx context.Context, dc *driverConn) (*driverStmt, error) {
	si, err := dc.prepareLocked(ctx, s.cg, s.query)
	if err != nil {
		return nil, err
	}
	cs := connStmt{dc, si}
	s.mu.Lock()
	s.css = append(s.css, cs)
	s.mu.Unlock()
	return cs.ds, nil
}

// QueryContext executes a prepared query statement with the given arguments
// and returns the query results as a *Rows.
func (s *Stmt) QueryContext(ctx context.Context, args ...interface{}) (*Rows, error) {
	s.closemu.RLock()
	defer s.closemu.RUnlock()

	var rowsi driver.Rows
	strategy := cachedOrNewConn
	for i := 0; i < maxBadConnRetries+1; i++ {
		if i == maxBadConnRetries {
			strategy = alwaysNewConn
		}
		dc, releaseConn, ds, err := s.connStmt(ctx, strategy)
		if err != nil {
			if err == driver.ErrBadConn {
				continue
			}
			return nil, err
		}

		rowsi, err = rowsiFromStatement(ctx, dc.ci, ds, args...)
		if err == nil {
			// Note: ownership of ci passes to the *Rows, to be freed
			// with releaseConn.
			rows := &Rows{
				dc:    dc,
				rowsi: rowsi,
				// releaseConn set below
			}
			// addDep must be added before initContextClose or it could attempt
			// to removeDep before it has been added.
			s.db.addDep(s, rows)

			// releaseConn must be set before initContextClose or it could
			// release the connection before it is set.
			rows.releaseConn = func(err error) {
				releaseConn(err)
				s.db.removeDep(s, rows)
			}
			var txctx context.Context
			if s.cg != nil {
				txctx = s.cg.txCtx()
			}
			rows.initContextClose(ctx, txctx)
			return rows, nil
		}

		releaseConn(err)
		if err != driver.ErrBadConn {
			return nil, err
		}
	}
	return nil, driver.ErrBadConn
}

// Query executes a prepared query statement with the given arguments
// and returns the query results as a *Rows.
func (s *Stmt) Query(args ...interface{}) (*Rows, error) {
	return s.QueryContext(context.Background(), args...)
}

func rowsiFromStatement(ctx context.Context, ci driver.Conn, ds *driverStmt, args ...interface{}) (driver.Rows, error) {
	ds.Lock()
	defer ds.Unlock()
	dargs, err := driverArgsConnLocked(ci, ds, args)
	if err != nil {
		return nil, err
	}
	return ctxDriverStmtQuery(ctx, ds.si, dargs)
}

// QueryRowContext executes a prepared query statement with the given arguments.
// If an error occurs during the execution of the statement, that error will
// be returned by a call to Scan on the returned *Row, which is always non-nil.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
func (s *Stmt) QueryRowContext(ctx context.Context, args ...interface{}) *Row {
	rows, err := s.QueryContext(ctx, args...)
	if err != nil {
		return &Row{err: err}
	}
	return &Row{rows: rows}
}

// QueryRow executes a prepared query statement with the given arguments.
// If an error occurs during the execution of the statement, that error will
// be returned by a call to Scan on the returned *Row, which is always non-nil.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
//
// Example usage:
//
//  var name string
//  err := nameByUseridStmt.QueryRow(id).Scan(&name)
func (s *Stmt) QueryRow(args ...interface{}) *Row {
	return s.QueryRowContext(context.Background(), args...)
}

// Close closes the statement.
func (s *Stmt) Close() error {
	s.closemu.Lock()
	defer s.closemu.Unlock()

	if s.stickyErr != nil {
		return s.stickyErr
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	txds := s.cgds
	s.cgds = nil

	s.mu.Unlock()

	if s.cg == nil {
		return s.db.removeDep(s, s)
	}

	if s.parentStmt != nil {
		// If parentStmt is set, we must not close s.txds since it's stored
		// in the css array of the parentStmt.
		return s.db.removeDep(s.parentStmt, s)
	}
	return txds.Close()
}

func (s *Stmt) finalClose() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.css != nil {
		for _, v := range s.css {
			s.db.noteUnusedDriverStatement(v.dc, v.ds)
			v.dc.removeOpenStmt(v.ds)
		}
		s.css = nil
	}
	return nil
}

// Rows is the result of a query. Its cursor starts before the first row
// of the result set. Use Next to advance from row to row.
type Rows struct {
	dc          *driverConn // owned; must call releaseConn when closed to release
	releaseConn func(error)
	rowsi       driver.Rows
	cancel      func()      // called when Rows is closed, may be nil.
	closeStmt   *driverStmt // if non-nil, statement to Close on close

	// closemu prevents Rows from closing while there
	// is an active streaming result. It is held for read during non-close operations
	// and exclusively during close.
	//
	// closemu guards lasterr and closed.
	closemu sync.RWMutex
	closed  bool
	lasterr error // non-nil only if closed is true

	// lastcols is only used in Scan, Next, and NextResultSet which are expected
	// not to be called concurrently.
	lastcols []driver.Value
}

// lasterrOrErrLocked returns either lasterr or the provided err.
// rs.closemu must be read-locked.
func (rs *Rows) lasterrOrErrLocked(err error) error {
	if rs.lasterr != nil && rs.lasterr != io.EOF {
		return rs.lasterr
	}
	return err
}

// bypassRowsAwaitDone is only used for testing.
// If true, it will not close the Rows automatically from the context.
var bypassRowsAwaitDone = false

func (rs *Rows) initContextClose(ctx, txctx context.Context) {
	if ctx.Done() == nil && (txctx == nil || txctx.Done() == nil) {
		return
	}
	if bypassRowsAwaitDone {
		return
	}
	ctx, rs.cancel = context.WithCancel(ctx)
	go rs.awaitDone(ctx, txctx)
}

// awaitDone blocks until either ctx or txctx is canceled. The ctx is provided
// from the query context and is canceled when the query Rows is closed.
// If the query was issued in a transaction, the transaction's context
// is also provided in txctx to ensure Rows is closed if the Tx is closed.
func (rs *Rows) awaitDone(ctx, txctx context.Context) {
	var txctxDone <-chan struct{}
	if txctx != nil {
		txctxDone = txctx.Done()
	}
	select {
	case <-ctx.Done():
	case <-txctxDone:
	}
	rs.close(ctx.Err())
}

// Next prepares the next result row for reading with the Scan method. It
// returns true on success, or false if there is no next result row or an error
// happened while preparing it. Err should be consulted to distinguish between
// the two cases.
//
// Every call to Scan, even the first one, must be preceded by a call to Next.
func (rs *Rows) Next() bool {
	var doClose, ok bool
	withLock(rs.closemu.RLocker(), func() {
		doClose, ok = rs.nextLocked()
	})
	if doClose {
		rs.Close()
	}
	return ok
}

func (rs *Rows) nextLocked() (doClose, ok bool) {
	if rs.closed {
		return false, false
	}

	// Lock the driver connection before calling the driver interface
	// rowsi to prevent a Tx from rolling back the connection at the same time.
	rs.dc.Lock()
	defer rs.dc.Unlock()

	if rs.lastcols == nil {
		rs.lastcols = make([]driver.Value, len(rs.rowsi.Columns()))
	}

	rs.lasterr = rs.rowsi.Next(rs.lastcols)
	if rs.lasterr != nil {
		// Close the connection if there is a driver error.
		if rs.lasterr != io.EOF {
			return true, false
		}
		nextResultSet, ok := rs.rowsi.(driver.RowsNextResultSet)
		if !ok {
			return true, false
		}
		// The driver is at the end of the current result set.
		// Test to see if there is another result set after the current one.
		// Only close Rows if there is no further result sets to read.
		if !nextResultSet.HasNextResultSet() {
			doClose = true
		}
		return doClose, false
	}
	return false, true
}

// NextResultSet prepares the next result set for reading. It reports whether
// there is further result sets, or false if there is no further result set
// or if there is an error advancing to it. The Err method should be consulted
// to distinguish between the two cases.
//
// After calling NextResultSet, the Next method should always be called before
// scanning. If there are further result sets they may not have rows in the result
// set.
func (rs *Rows) NextResultSet() bool {
	var doClose bool
	defer func() {
		if doClose {
			rs.Close()
		}
	}()
	rs.closemu.RLock()
	defer rs.closemu.RUnlock()

	if rs.closed {
		return false
	}

	rs.lastcols = nil
	nextResultSet, ok := rs.rowsi.(driver.RowsNextResultSet)
	if !ok {
		doClose = true
		return false
	}

	// Lock the driver connection before calling the driver interface
	// rowsi to prevent a Tx from rolling back the connection at the same time.
	rs.dc.Lock()
	defer rs.dc.Unlock()

	rs.lasterr = nextResultSet.NextResultSet()
	if rs.lasterr != nil {
		doClose = true
		return false
	}
	return true
}

// Err returns the error, if any, that was encountered during iteration.
// Err may be called after an explicit or implicit Close.
func (rs *Rows) Err() error {
	rs.closemu.RLock()
	defer rs.closemu.RUnlock()
	return rs.lasterrOrErrLocked(nil)
}

var errRowsClosed = errors.New("sql: Rows are closed")
var errNoRows = errors.New("sql: no Rows available")

// Columns returns the column names.
// Columns returns an error if the rows are closed.
func (rs *Rows) Columns() ([]string, error) {
	rs.closemu.RLock()
	defer rs.closemu.RUnlock()
	if rs.closed {
		return nil, rs.lasterrOrErrLocked(errRowsClosed)
	}
	if rs.rowsi == nil {
		return nil, rs.lasterrOrErrLocked(errNoRows)
	}
	rs.dc.Lock()
	defer rs.dc.Unlock()

	return rs.rowsi.Columns(), nil
}

// ColumnTypes returns column information such as column type, length,
// and nullable. Some information may not be available from some drivers.
func (rs *Rows) ColumnTypes() ([]*ColumnType, error) {
	rs.closemu.RLock()
	defer rs.closemu.RUnlock()
	if rs.closed {
		return nil, rs.lasterrOrErrLocked(errRowsClosed)
	}
	if rs.rowsi == nil {
		return nil, rs.lasterrOrErrLocked(errNoRows)
	}
	rs.dc.Lock()
	defer rs.dc.Unlock()

	return rowsColumnInfoSetupConnLocked(rs.rowsi), nil
}

// ColumnType contains the name and type of a column.
type ColumnType struct {
	name string

	hasNullable       bool
	hasLength         bool
	hasPrecisionScale bool

	nullable     bool
	length       int64
	databaseType string
	precision    int64
	scale        int64
	scanType     reflect.Type
}

// Name returns the name or alias of the column.
func (ci *ColumnType) Name() string {
	return ci.name
}

// Length returns the column type length for variable length column types such
// as text and binary field types. If the type length is unbounded the value will
// be math.MaxInt64 (any database limits will still apply).
// If the column type is not variable length, such as an int, or if not supported
// by the driver ok is false.
func (ci *ColumnType) Length() (length int64, ok bool) {
	return ci.length, ci.hasLength
}

// DecimalSize returns the scale and precision of a decimal type.
// If not applicable or if not supported ok is false.
func (ci *ColumnType) DecimalSize() (precision, scale int64, ok bool) {
	return ci.precision, ci.scale, ci.hasPrecisionScale
}

// ScanType returns a Go type suitable for scanning into using Rows.Scan.
// If a driver does not support this property ScanType will return
// the type of an empty interface.
func (ci *ColumnType) ScanType() reflect.Type {
	return ci.scanType
}

// Nullable reports whether the column may be null.
// If a driver does not support this property ok will be false.
func (ci *ColumnType) Nullable() (nullable, ok bool) {
	return ci.nullable, ci.hasNullable
}

// DatabaseTypeName returns the database system name of the column type. If an empty
// string is returned, then the driver type name is not supported.
// Consult your driver documentation for a list of driver data types. Length specifiers
// are not included.
// Common type names include "VARCHAR", "TEXT", "NVARCHAR", "DECIMAL", "BOOL",
// "INT", and "BIGINT".
func (ci *ColumnType) DatabaseTypeName() string {
	return ci.databaseType
}

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

// Scan copies the columns in the current row into the values pointed
// at by dest. The number of values in dest must be the same as the
// number of columns in Rows.
//
// Scan converts columns read from the database into the following
// common Go types and special types provided by the sql package:
//
//    *string
//    *[]byte
//    *int, *int8, *int16, *int32, *int64
//    *uint, *uint8, *uint16, *uint32, *uint64
//    *bool
//    *float32, *float64
//    *interface{}
//    *RawBytes
//    *Rows (cursor value)
//    any type implementing Scanner (see Scanner docs)
//
// In the most simple case, if the type of the value from the source
// column is an integer, bool or string type T and dest is of type *T,
// Scan simply assigns the value through the pointer.
//
// Scan also converts between string and numeric types, as long as no
// information would be lost. While Scan stringifies all numbers
// scanned from numeric database columns into *string, scans into
// numeric types are checked for overflow. For example, a float64 with
// value 300 or a string with value "300" can scan into a uint16, but
// not into a uint8, though float64(255) or "255" can scan into a
// uint8. One exception is that scans of some float64 numbers to
// strings may lose information when stringifying. In general, scan
// floating point columns into *float64.
//
// If a dest argument has type *[]byte, Scan saves in that argument a
// copy of the corresponding data. The copy is owned by the caller and
// can be modified and held indefinitely. The copy can be avoided by
// using an argument of type *RawBytes instead; see the documentation
// for RawBytes for restrictions on its use.
//
// If an argument has type *interface{}, Scan copies the value
// provided by the underlying driver without conversion. When scanning
// from a source value of type []byte to *interface{}, a copy of the
// slice is made and the caller owns the result.
//
// Source values of type time.Time may be scanned into values of type
// *time.Time, *interface{}, *string, or *[]byte. When converting to
// the latter two, time.RFC3339Nano is used.
//
// Source values of type bool may be scanned into types *bool,
// *interface{}, *string, *[]byte, or *RawBytes.
//
// For scanning into *bool, the source may be true, false, 1, 0, or
// string inputs parseable by strconv.ParseBool.
//
// Scan can also convert a cursor returned from a query, such as
// "select cursor(select * from my_table) from dual", into a
// *Rows value that can itself be scanned from. The parent
// select query will close any cursor *Rows if the parent *Rows is closed.
//
// If any of the first arguments implementing Scanner returns an error,
// that error will be wrapped in the returned error
func (rs *Rows) Scan(dest ...interface{}) error {
	rs.closemu.RLock()

	if rs.lasterr != nil && rs.lasterr != io.EOF {
		rs.closemu.RUnlock()
		return rs.lasterr
	}
	if rs.closed {
		err := rs.lasterrOrErrLocked(errRowsClosed)
		rs.closemu.RUnlock()
		return err
	}
	rs.closemu.RUnlock()

	if rs.lastcols == nil {
		return errors.New("sql: Scan called without calling Next")
	}
	if len(dest) != len(rs.lastcols) {
		return fmt.Errorf("sql: expected %d destination arguments in Scan, not %d", len(rs.lastcols), len(dest))
	}
	for i, sv := range rs.lastcols {
		err := convertAssignRows(dest[i], sv, rs)
		if err != nil {
			return fmt.Errorf(`sql: Scan error on column index %d, name %q: %w`, i, rs.rowsi.Columns()[i], err)
		}
	}
	return nil
}

// rowsCloseHook returns a function so tests may install the
// hook through a test only mutex.
var rowsCloseHook = func() func(*Rows, *error) { return nil }

// Close closes the Rows, preventing further enumeration. If Next is called
// and returns false and there are no further result sets,
// the Rows are closed automatically and it will suffice to check the
// result of Err. Close is idempotent and does not affect the result of Err.
func (rs *Rows) Close() error {
	return rs.close(nil)
}

func (rs *Rows) close(err error) error {
	rs.closemu.Lock()
	defer rs.closemu.Unlock()

	if rs.closed {
		return nil
	}
	rs.closed = true

	if rs.lasterr == nil {
		rs.lasterr = err
	}

	withLock(rs.dc, func() {
		err = rs.rowsi.Close()
	})
	if fn := rowsCloseHook(); fn != nil {
		fn(rs, &err)
	}
	if rs.cancel != nil {
		rs.cancel()
	}

	if rs.closeStmt != nil {
		rs.closeStmt.Close()
	}
	rs.releaseConn(err)
	return err
}

// Row is the result of calling QueryRow to select a single row.
type Row struct {
	// One of these two will be non-nil:
	err  error // deferred error for easy chaining
	rows *Rows
}

// Scan copies the columns from the matched row into the values
// pointed at by dest. See the documentation on Rows.Scan for details.
// If more than one row matches the query,
// Scan uses the first row and discards the rest. If no row matches
// the query, Scan returns ErrNoRows.
func (r *Row) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}

	// TODO(bradfitz): for now we need to defensively clone all
	// []byte that the driver returned (not permitting
	// *RawBytes in Rows.Scan), since we're about to close
	// the Rows in our defer, when we return from this function.
	// the contract with the driver.Next(...) interface is that it
	// can return slices into read-only temporary memory that's
	// only valid until the next Scan/Close. But the TODO is that
	// for a lot of drivers, this copy will be unnecessary. We
	// should provide an optional interface for drivers to
	// implement to say, "don't worry, the []bytes that I return
	// from Next will not be modified again." (for instance, if
	// they were obtained from the network anyway) But for now we
	// don't care.
	defer r.rows.Close()
	for _, dp := range dest {
		if _, ok := dp.(*RawBytes); ok {
			return errors.New("sql: RawBytes isn't allowed on Row.Scan")
		}
	}

	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return ErrNoRows
	}
	err := r.rows.Scan(dest...)
	if err != nil {
		return err
	}
	// Make sure the query can be processed to completion with no errors.
	return r.rows.Close()
}

// Err provides a way for wrapping packages to check for
// query errors without calling Scan.
// Err returns the error, if any, that was encountered while running the query.
// If this error is not nil, this error will also be returned from Scan.
func (r *Row) Err() error {
	return r.err
}

// A Result summarizes an executed SQL command.
type Result interface {
	// LastInsertId returns the integer generated by the database
	// in response to a command. Typically this will be from an
	// "auto increment" column when inserting a new row. Not all
	// databases support this feature, and the syntax of such
	// statements varies.
	LastInsertId() (int64, error)

	// RowsAffected returns the number of rows affected by an
	// update, insert, or delete. Not every database or database
	// driver may support this.
	RowsAffected() (int64, error)
}

type driverResult struct {
	sync.Locker // the *driverConn
	resi        driver.Result
}

func (dr driverResult) LastInsertId() (int64, error) {
	dr.Lock()
	defer dr.Unlock()
	return dr.resi.LastInsertId()
}

func (dr driverResult) RowsAffected() (int64, error) {
	dr.Lock()
	defer dr.Unlock()
	return dr.resi.RowsAffected()
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
