package cpool

import (
	"context"
	"cpool/driver"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
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

	// guarded by connPool.poolFacade.mu
	inUse      bool
	returnedAt time.Time // Time the connection was created or returned.
	onPut      []func()  // code (with connPool.poolFacade.mu held) run when conn is next returned
	dbmuClosed bool      // same as closed, but guarded by connPool.poolFacade.mu, for removeClosedStmtLocked
}

//TODO изучить когда вызывается
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
	log.Println("driverConn Close start")
	dc.Lock()

	dc.connPool.poolFacade.removeConnFromLru(dc)
	if dc.closed {
		dc.Unlock()
		return errors.New("sql: duplicate driverConn close")
	}
	dc.closed = true
	dc.Unlock() // not defer; removeDep finalClose calls may need to lock

	// And now updates that require holding dc.mu.Lock.
	dc.connPool.poolFacade.mu.Lock()
	dc.dbmuClosed = true
	fn := dc.connPool.removeDepLocked(dc, dc)
	dc.connPool.poolFacade.mu.Unlock()

	log.Println("driverConn Close end")
	return fn()
}

//TODO изучить / закрытие соединения
func (dc *driverConn) finalClose() error {
	log.Println("warning final close")
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

	//dc.connPool.poolFacade.mu.Lock()
	//dc.connPool.numOpened--
	dc.connPool.poolFacade.decrementNumOpened()
	//TODO попытка открыть новые соединения
	dc.connPool.poolFacade.maybeOpenNewConnections()
	//dc.connPool.poolFacade.mu.Unlock()

	log.Println("warning final close 2")

	atomic.AddUint64(&dc.connPool.numClosed, 1)
	log.Println("warning final close 3")
	return err
}
