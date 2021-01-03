package cpool

import (
	"context"
	"cpool/driver"
	"sync/atomic"
)

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
