package cpool

import (
	"context"
	"cpool/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ConnPool is a database handle representing a pool of zero or more
// underlying connections. It's safe for concurrent use by multiple
// goroutines.
//
// The sql package creates and frees connections automatically; it
// also maintains a free pool of idle connections. If the database has
// a concept of per-connection state, such state can be reliably observed
// within a transaction (Tx) or connection (Conn). Once ConnPool.Begin is called, the
// returned Tx is bound to a single connection. Once Commit or
// Rollback is called on the transaction, that transaction's
// connection is returned to ConnPool's idle connection pool. The pool size
// can be controlled with SetMaxIdleConns.
type ConnPool struct {
	poolFacade *PoolFacade

	// Atomic access only. At top of struct to prevent mis-alignment
	// on 32-bit platforms. Of type time.Duration.
	waitDuration int64 // Total time waited for new connections.

	connector driver.Connector
	// numClosed is an atomic counter which represents a total number of
	// closed connections. Stmt.openStmt checks it before cleaning closed
	// connections in Stmt.css.
	numClosed uint64

	mu       sync.Mutex // protects following fields
	freeConn []*driverConn
	//connRequests map[uint64]chan connCreationResponse
	//nextRequest  uint64 // Next key to use in connRequests.
	//TODO numInUse + numIdle
	//numOpen int // number of opened and pending open connections
	// Used to signal the need for new connections
	// a goroutine running connectionOpener() reads on this chan and
	// maybeOpenNewConnectionsLocked sends on the chan (one send per needed connection)
	// It is closed during connPool.Close(). The close tells the connectionOpener
	// goroutine to exit.
	openerCh chan struct{}

	closed       bool //TODO возможно стоит убрать
	dep          map[finalCloser]depSet
	lastPut      map[*driverConn]string // stacktrace of last conn's put; debug only
	maxIdleCount int                    // zero means defaultMaxIdleConns; negative means 0
	//TODO unlimited
	//maxOpen           int                    // <= 0 means unlimited
	maxLifetime time.Duration // maximum amount of time a connection may be reused
	maxIdleTime time.Duration // maximum amount of time a connection may be idle before being closed

	cleanerCh chan struct{} //TODO что это maxLifetime was changed or connPool was closed.
	//waitCount         int64 // Total number of connections waited for.
	maxIdleClosed     int64 // Total number of connections closed due to idle count.
	maxIdleTimeClosed int64 // Total number of connections closed due to idle time.
	maxLifetimeClosed int64 // Total number of connections closed due to max connection lifetime limit.

	stop func() // stop cancels the connection opener.
}

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

	if connPool.poolFacade.isOpenConnectionLimitExceeded() {
		// TODO try to remove idle connection from another connection pool and
		//  wait for free connection

		// Make the connCreationResponse channel. It's buffered so that the
		// connectionOpener doesn't block while waiting for the responseChan to be read.

		responseChan := make(chan connCreationResponse, 1)
		reqKey := connPool.poolFacade.nextRequestKeyLocked()

		connPool.poolFacade.mu.Lock()
		connPool.poolFacade.connRequests[reqKey] = connRequest{
			connPool: connPool,
			responce: responseChan,
		}
		connPool.poolFacade.waitCount++
		connPool.poolFacade.mu.Unlock()
		connPool.mu.Unlock()

		waitStart := nowFunc()

		// Timeout the connection request with the context.
		select {
		//Context expired
		case <-ctx.Done():
			// Remove the connection request and ensure no value has been sent
			// on it after removing.
			connPool.poolFacade.mu.Lock()
			delete(connPool.poolFacade.connRequests, reqKey)
			connPool.poolFacade.mu.Unlock()

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

	connPool.poolFacade.incrementNumOpenedLocked()
	//connPool.numOpen++ // optimistically
	connPool.mu.Unlock()
	//Создание нового соединения
	ci, err := connPool.connector.Connect(ctx)
	if err != nil {
		connPool.mu.Lock()
		connPool.poolFacade.decrementNumOpenedLocked()
		//connPool.numOpen-- // correct for earlier optimism
		//TODO попытка открыть новые соедиения
		connPool.poolFacade.maybeOpenNewConnectionsLocked()
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
		connPool.poolFacade.maybeOpenNewConnectionsLocked()
		connPool.mu.Unlock()
		dc.Close()
		return
	}
	if putConnHook != nil {
		putConnHook(connPool, dc)
	}
	added := connPool.putConnectionConnPoolLocked(dc, nil)
	connPool.mu.Unlock()

	if !added {
		dc.Close()
		return
	}
}

// Satisfy a connCreationResponse or put the driverConn in the idle pool and return true
// or return false.
// putConnectionConnPoolLocked will satisfy a connCreationResponse if there is one, or it will
// return the *driverConn to the freeConn list if err == nil and the idle
// connection limit will not be exceeded.
// If err != nil, the value of dc is ignored.
// If err == nil, then dc must not equal nil.
// If a connCreationResponse was fulfilled or the *driverConn was placed in the
// freeConn list, then true is returned, otherwise false is returned.
func (connPool *ConnPool) putConnectionConnPoolLocked(dc *driverConn, err error) bool {
	if connPool.closed {
		return false
	}
	if connPool.poolFacade.isOpenConnectionLimitExceeded() {
		return false
	}
	if c := len(connPool.poolFacade.connRequests); c > 0 {
		var reqKey uint64
		var req chan connCreationResponse
		for key, value := range connPool.poolFacade.connRequests {
			reqKey = key
			req = value.responce
			break
		}

		delete(connPool.poolFacade.connRequests, reqKey) // Remove from pending requests.
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
