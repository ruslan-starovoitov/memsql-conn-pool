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
package sql

import (
	"context"
	"errors"
	"fmt"
	"memsql-conn-pool/driver"
	"runtime"
	"sort"
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

// TxOptions holds the transaction options to be used in DB.BeginTx.
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
//  err := db.QueryRow("SELECT name FROM foo WHERE id=?", id).Scan(&s)
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
//   _, err := db.ExecContext(ctx, "ProcName", sql.Named("Arg1", sql.Out{Dest: &outArg}))
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

// driverConn wraps a driver.Conn with a mutex, to
// be held during all calls into the Conn. (including any calls onto
// interfaces returned via that Conn, such as calls on Tx, Stmt,
// Result, Rows)
type driverConn struct {
	db        *DB
	createdAt time.Time

	sync.Mutex  // guards following
	ci          driver.Conn
	needReset   bool // The connection session should be reset before use if true.
	closed      bool
	finalClosed bool // ci.Close has been called
	openStmt    map[*driverStmt]bool

	// guarded by db.mu
	inUse      bool
	returnedAt time.Time // Time the connection was created or returned.
	onPut      []func()  // code (with db.mu held) run when conn is next returned
	dbmuClosed bool      // same as closed, but guarded by db.mu, for removeClosedStmtLocked
}

func (dc *driverConn) releaseConn(err error) {
	dc.db.putConn(dc, err, true)
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

// the dc.db's Mutex is held.
func (dc *driverConn) closeDBLocked() func() error {
	dc.Lock()
	defer dc.Unlock()
	if dc.closed {
		return func() error { return errors.New("sql: duplicate driverConn close") }
	}
	dc.closed = true
	return dc.db.removeDepLocked(dc, dc)
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
	dc.db.mu.Lock()
	dc.dbmuClosed = true
	fn := dc.db.removeDepLocked(dc, dc)
	dc.db.mu.Unlock()
	return fn()
}

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

	dc.db.mu.Lock()
	dc.db.numOpen--
	dc.db.maybeOpenNewConnections()
	dc.db.mu.Unlock()

	atomic.AddUint64(&dc.db.numClosed, 1)
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

// The finalCloser interface is used by (*DB).addDep and related
// dependency reference counting.
type finalCloser interface {
	// finalClose is called when the reference count of an object
	// goes to zero. (*DB).mu is not held while calling it.
	finalClose() error
}

// addDep notes that x now depends on dep, and x's finalClose won't be
// called until all of x's dependencies are removed with removeDep.
func (db *DB) addDep(x finalCloser, dep interface{}) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.addDepLocked(x, dep)
}

func (db *DB) addDepLocked(x finalCloser, dep interface{}) {
	if db.dep == nil {
		db.dep = make(map[finalCloser]depSet)
	}
	xdep := db.dep[x]
	if xdep == nil {
		xdep = make(depSet)
		db.dep[x] = xdep
	}
	xdep[dep] = true
}

// removeDep notes that x no longer depends on dep.
// If x still has dependencies, nil is returned.
// If x no longer has any dependencies, its finalClose method will be
// called and its error value will be returned.
func (db *DB) removeDep(x finalCloser, dep interface{}) error {
	db.mu.Lock()
	fn := db.removeDepLocked(x, dep)
	db.mu.Unlock()
	return fn()
}

func (db *DB) removeDepLocked(x finalCloser, dep interface{}) func() error {

	xdep, ok := db.dep[x]
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
		delete(db.dep, x)
		return x.finalClose
	default:
		// Dependencies remain.
		return func() error { return nil }
	}
}

// This is the size of the connectionOpener request chan (DB.openerCh).
// This value should be larger than the maximum typical value
// used for db.maxOpen. If maxOpen is significantly larger than
// connectionRequestQueueSize then it is possible for ALL calls into the *DB
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
// helper function that returns a *DB. No database drivers are included
// in the Go standard library. See https://golang.org/s/sqldrivers for
// a list of third-party drivers.
//
// OpenDB may just validate its arguments without creating a connection
// to the database. To verify that the data source name is valid, call
// Ping.
//
// The returned DB is safe for concurrent use by multiple goroutines
// and maintains its own pool of idle connections. Thus, the OpenDB
// function should be called just once. It is rarely necessary to
// close a DB.
func OpenDB(c driver.Connector) *DB {
	ctx, cancel := context.WithCancel(context.Background())
	db := &DB{
		connector:    c,
		openerCh:     make(chan struct{}, connectionRequestQueueSize),
		lastPut:      make(map[*driverConn]string),
		connRequests: make(map[uint64]chan connRequest),
		stop:         cancel,
	}

	go db.connectionOpener(ctx)

	return db
}

// Open opens a database specified by its database driver name and a
// driver-specific data source name, usually consisting of at least a
// database name and connection information.
//
// Most users will open a database via a driver-specific connection
// helper function that returns a *DB. No database drivers are included
// in the Go standard library. See https://golang.org/s/sqldrivers for
// a list of third-party drivers.
//
// Open may just validate its arguments without creating a connection
// to the database. To verify that the data source name is valid, call
// Ping.
//
// The returned DB is safe for concurrent use by multiple goroutines
// and maintains its own pool of idle connections. Thus, the Open
// function should be called just once. It is rarely necessary to
// close a DB.
func Open(driverName, dataSourceName string) (*DB, error) {
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

// connRequest represents one request for a new connection
// When there are no idle connections available, DB.conn will create
// a new connRequest and put it on the db.connRequests list.
type connRequest struct {
	conn *driverConn
	err  error
}

var errDBClosed = errors.New("sql: database is closed")

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (db *DB) nextRequestKeyLocked() uint64 {
	next := db.nextRequest
	db.nextRequest++
	return next
}

// conn returns a newly-opened or cached *driverConn.
func (db *DB) conn(ctx context.Context, strategy connReuseStrategy) (*driverConn, error) {
	db.mu.Lock()
	if db.closed {
		db.mu.Unlock()
		return nil, errDBClosed
	}
	// Check if the context is expired.
	select {
	default:
	case <-ctx.Done():
		db.mu.Unlock()
		return nil, ctx.Err()
	}
	lifetime := db.maxLifetime

	//ignore this strategy
	// Prefer a free connection, if possible.
	//numFree := len(db.freeConn)
	//if strategy == cachedOrNewConn && numFree > 0 {
	//	conn := db.freeConn[0]
	//	copy(db.freeConn, db.freeConn[1:])
	//	db.freeConn = db.freeConn[:numFree-1]
	//	conn.inUse = true
	//	if conn.expired(lifetime) {
	//		db.maxLifetimeClosed++
	//		db.mu.Unlock()
	//		conn.Close()
	//		return nil, driver.ErrBadConn
	//	}
	//	db.mu.Unlock()
	//
	//	// Reset the session if required.
	//	if err := conn.resetSession(ctx); err == driver.ErrBadConn {
	//		conn.Close()
	//		return nil, driver.ErrBadConn
	//	}
	//
	//	return conn, nil
	//}

	// Out of free connections or we were asked not to use one. If we're not
	// allowed to open any more connections, make a request and wait.
	if db.maxOpen > 0 && db.maxOpen <= db.numOpen {
		// Make the connRequest channel. It's buffered so that the
		// connectionOpener doesn't block while waiting for the req to be read.
		req := make(chan connRequest, 1)
		reqKey := db.nextRequestKeyLocked()
		db.connRequests[reqKey] = req
		db.waitCount++
		db.mu.Unlock()

		waitStart := nowFunc()

		// Timeout the connection request with the context.
		select {
		case <-ctx.Done():
			// Remove the connection request and ensure no value has been sent
			// on it after removing.
			db.mu.Lock()
			delete(db.connRequests, reqKey)
			db.mu.Unlock()

			atomic.AddInt64(&db.waitDuration, int64(time.Since(waitStart)))

			select {
			default:
			case ret, ok := <-req:
				if ok && ret.conn != nil {
					db.putConn(ret.conn, ret.err, false)
				}
			}
			return nil, ctx.Err()
		case ret, ok := <-req:
			atomic.AddInt64(&db.waitDuration, int64(time.Since(waitStart)))

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
				db.mu.Lock()
				db.maxLifetimeClosed++
				db.mu.Unlock()
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

	db.numOpen++ // optimistically
	db.mu.Unlock()
	ci, err := db.connector.Connect(ctx)
	if err != nil {
		db.mu.Lock()
		db.numOpen-- // correct for earlier optimism
		db.maybeOpenNewConnections()
		db.mu.Unlock()
		return nil, err
	}
	db.mu.Lock()
	dc := &driverConn{
		db:         db,
		createdAt:  nowFunc(),
		returnedAt: nowFunc(),
		ci:         ci,
		inUse:      true,
	}
	db.addDepLocked(dc, dc)
	db.mu.Unlock()
	return dc, nil
}

// putConnHook is a hook for testing.
var putConnHook func(*DB, *driverConn)

// noteUnusedDriverStatement notes that ds is no longer used and should
// be closed whenever possible (when c is next not in use), unless c is
// already closed.
func (db *DB) noteUnusedDriverStatement(c *driverConn, ds *driverStmt) {
	db.mu.Lock()
	defer db.mu.Unlock()
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

// putConn adds a connection to the db's free pool.
// err is optionally the last error that occurred on this connection.
func (db *DB) putConn(dc *driverConn, err error, resetSession bool) {
	if err != driver.ErrBadConn {
		if !dc.validateConnection(resetSession) {
			err = driver.ErrBadConn
		}
	}
	db.mu.Lock()
	if !dc.inUse {
		db.mu.Unlock()
		if debugGetPut {
			fmt.Printf("putConn(%v) DUPLICATE was: %s\n\nPREVIOUS was: %s", dc, stack(), db.lastPut[dc])
		}
		panic("sql: connection returned that was never out")
	}

	if err != driver.ErrBadConn && dc.expired(db.maxLifetime) {
		db.maxLifetimeClosed++
		err = driver.ErrBadConn
	}
	if debugGetPut {
		db.lastPut[dc] = stack()
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
		db.maybeOpenNewConnections()
		db.mu.Unlock()
		dc.Close()
		return
	}
	if putConnHook != nil {
		putConnHook(db, dc)
	}
	added := db.putConnDBLocked(dc, nil)
	db.mu.Unlock()

	if !added {
		dc.Close()
		return
	}
}

// Satisfy a connRequest or put the driverConn in the idle pool and return true
// or return false.
// putConnDBLocked will satisfy a connRequest if there is one, or it will
// return the *driverConn to the freeConn list if err == nil and the idle
// connection limit will not be exceeded.
// If err != nil, the value of dc is ignored.
// If err == nil, then dc must not equal nil.
// If a connRequest was fulfilled or the *driverConn was placed in the
// freeConn list, then true is returned, otherwise false is returned.
func (db *DB) putConnDBLocked(dc *driverConn, err error) bool {
	if db.closed {
		return false
	}
	if db.maxOpen > 0 && db.numOpen > db.maxOpen {
		return false
	}
	if c := len(db.connRequests); c > 0 {
		var req chan connRequest
		var reqKey uint64
		for reqKey, req = range db.connRequests {
			break
		}
		delete(db.connRequests, reqKey) // Remove from pending requests.
		if err == nil {
			dc.inUse = true
		}
		req <- connRequest{
			conn: dc,
			err:  err,
		}
		return true
	} else if err == nil && !db.closed {
		if db.maxIdleConnsLocked() > len(db.freeConn) {
			db.freeConn = append(db.freeConn, dc)
			db.startCleanerLocked()
			return true
		}
		db.maxIdleClosed++
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
func (db *DB) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	var stmt *Stmt
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		stmt, err = db.prepare(ctx, query, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		return db.prepare(ctx, query, alwaysNewConn)
	}
	return stmt, err
}

// Prepare creates a prepared statement for later queries or executions.
// Multiple queries or executions may be run concurrently from the
// returned statement.
// The caller must call the statement's Close method
// when the statement is no longer needed.
func (db *DB) Prepare(query string) (*Stmt, error) {
	return db.PrepareContext(context.Background(), query)
}

func (db *DB) prepare(ctx context.Context, query string, strategy connReuseStrategy) (*Stmt, error) {
	// TODO: check if db.driver supports an optional
	// driver.Preparer interface and call that instead, if so,
	// otherwise we make a prepared statement that's bound
	// to a connection, and to execute this prepared statement
	// we either need to use this connection (if it's free), else
	// get a new connection + re-prepare + execute on that one.
	dc, err := db.conn(ctx, strategy)
	if err != nil {
		return nil, err
	}
	return db.prepareDC(ctx, dc, dc.releaseConn, nil, query)
}

// prepareDC prepares a query on the driverConn and calls release before
// returning. When cg == nil it implies that a connection pool is used, and
// when cg != nil only a single driver connection is used.
func (db *DB) prepareDC(ctx context.Context, dc *driverConn, release func(error), cg stmtConnGrabber, query string) (*Stmt, error) {
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
		db:    db,
		query: query,
		cg:    cg,
		cgds:  ds,
	}

	// When cg == nil this statement will need to keep track of various
	// connections they are prepared on and record the stmt dependency on
	// the DB.
	if cg == nil {
		stmt.css = []connStmt{{dc, ds}}
		stmt.lastNumClosed = atomic.LoadUint64(&db.numClosed)
		db.addDep(stmt, stmt)
	}
	return stmt, nil
}

// ExecContext executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
func (db *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (Result, error) {
	var res Result
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		res, err = db.exec(ctx, query, args, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		return db.exec(ctx, query, args, alwaysNewConn)
	}
	return res, err
}

// Exec executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
func (db *DB) Exec(query string, args ...interface{}) (Result, error) {
	return db.ExecContext(context.Background(), query, args...)
}

func (db *DB) exec(ctx context.Context, query string, args []interface{}, strategy connReuseStrategy) (Result, error) {
	dc, err := db.conn(ctx, strategy)
	if err != nil {
		return nil, err
	}
	return db.execDC(ctx, dc, dc.releaseConn, query, args)
}

func (db *DB) execDC(ctx context.Context, dc *driverConn, release func(error), query string, args []interface{}) (res Result, err error) {
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
func (db *DB) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	var rows *Rows
	var err error
	//TODO choose a connection resuse strategy
	//for i := 0; i < maxBadConnRetries; i++ {
	//	rows, err = db.query(ctx, query, args, cachedOrNewConn)
	//	if err != driver.ErrBadConn {
	//		break
	//	}
	//}
	//if err == driver.ErrBadConn {
	//	return db.query(ctx, query, args, alwaysNewConn)
	//}

	for i := 0; i < maxBadConnRetries; i++ {
		rows, err = db.query(ctx, query, args, newOrCached)
		if err != driver.ErrBadConn {
			break
		}
	}

	return rows, err
}

// Query executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
func (db *DB) Query(query string, args ...interface{}) (*Rows, error) {
	return db.QueryContext(context.Background(), query, args...)
}

func (db *DB) query(ctx context.Context, query string, args []interface{}, strategy connReuseStrategy) (*Rows, error) {
	dc, err := db.conn(ctx, strategy)
	if err != nil {
		return nil, err
	}

	return db.queryDC(ctx, nil, dc, dc.releaseConn, query, args)
}

// queryDC executes a query on the given connection.
// The connection gets released by the releaseConn function.
// The ctx context is from a query method and the txctx context is from an
// optional transaction context.
func (db *DB) queryDC(ctx, txctx context.Context, dc *driverConn, releaseConn func(error), query string, args []interface{}) (*Rows, error) {
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
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := db.QueryContext(ctx, query, args...)
	return &Row{rows: rows, err: err}
}

// QueryRow executes a query that is expected to return at most one row.
// QueryRow always returns a non-nil value. Errors are deferred until
// Row's Scan method is called.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
func (db *DB) QueryRow(query string, args ...interface{}) *Row {
	return db.QueryRowContext(context.Background(), query, args...)
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
func (db *DB) BeginTx(ctx context.Context, opts *TxOptions) (*Tx, error) {
	var tx *Tx
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		tx, err = db.begin(ctx, opts, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		return db.begin(ctx, opts, alwaysNewConn)
	}
	return tx, err
}

// Begin starts a transaction. The default isolation level is dependent on
// the driver.
func (db *DB) Begin() (*Tx, error) {
	return db.BeginTx(context.Background(), nil)
}

func (db *DB) begin(ctx context.Context, opts *TxOptions, strategy connReuseStrategy) (tx *Tx, err error) {
	dc, err := db.conn(ctx, strategy)
	if err != nil {
		return nil, err
	}
	return db.beginDC(ctx, dc, dc.releaseConn, opts)
}

// beginDC starts a transaction. The provided dc must be valid and ready to use.
func (db *DB) beginDC(ctx context.Context, dc *driverConn, release func(error), opts *TxOptions) (tx *Tx, err error) {
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
		db:                 db,
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
func (db *DB) Driver() driver.Driver {
	return db.connector.Driver()
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
func (db *DB) Conn(ctx context.Context) (*Conn, error) {
	var dc *driverConn
	var err error
	for i := 0; i < maxBadConnRetries; i++ {
		dc, err = db.conn(ctx, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		dc, err = db.conn(ctx, alwaysNewConn)
	}
	if err != nil {
		return nil, err
	}

	conn := &Conn{
		db: db,
		dc: dc,
	}
	return conn, nil
}

type releaseConn func(error)

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
