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
	poolFacade *ConnPoolFacade

	// Atomic access only. At top of struct to prevent mis-alignment
	// on 32-bit platforms. Of type time.Duration.
	waitDuration int64 // Total time waited for new connections.

	connector driver.Connector
	// numClosed is an atomic counter which represents a total number of
	// closed connections. Stmt.openStmt checks it before cleaning closed
	// connections in Stmt.css.
	numClosed uint64

	mu sync.Mutex // protects following fields
	//freeConn []*driverConn
	freeConn     map[*driverConn]struct{}
	connRequests map[uint64]chan connCreationResponse
	nextRequest  uint64 // Next key to use in connRequests.
	//TODO numInUse + numIdle
	numOpen int // number of opened and pending open connections
	// Used to signal the need for new connections
	// a goroutine running connectionOpener() reads on this chan and
	// maybeOpenNewConnections sends on the chan (one send per needed connection)
	// It is closed during connPool.Close(). The close tells the connectionOpener
	// goroutine to exit.
	openerCh chan struct{}

	closed  bool //TODO возможно стоит убрать
	dep     map[finalCloser]depSet
	lastPut map[*driverConn]string // stacktrace of last conn's put; debug only
	//maxIdleCount int                    // zero means defaultMaxIdleConns; negative means 0
	//TODO unlimited
	//maxOpen           int                    // <= 0 means unlimited
	maxLifetime time.Duration // maximum amount of time a connection may be reused
	maxIdleTime time.Duration // maximum amount of time a connection may be idle before being closed

	cleanerCh chan struct{} //TODO что это maxLifetime was changed or connPool was closed.
	//waitCount         int64         // Total number of connections waited for.
	//waitCount         int   // Total number of connections waited for.
	maxIdleClosed     int64 // Total number of connections closed due to idle count.
	maxIdleTimeClosed int64 // Total number of connections closed due to idle time.
	maxLifetimeClosed int64 // Total number of connections closed due to max connection lifetime limit.

	stop func() // stop cancels the connection opener.
}

//TODO метод создаёт новое соединение и вызывает put
// Open one new connection
func (connPool *ConnPool) openNewConnection(ctx context.Context) {
	log.Println("ConnPool openNewConnection")

	// maybeOpenNewConnections has already executed connPool.numOpened++ before it sent
	// on connPool.openerCh. This function must execute connPool.numOpened-- if the
	// connection fails or is closed before returning.

	//TODO вызов внутренней функции драйвера mysql
	// внутри происходит авторизация
	// это создание нового соединения
	// занимает много времени
	conn, err := connPool.connector.Connect(ctx)

	connPool.mu.Lock()
	defer connPool.mu.Unlock()

	if connPool.closed {
		if err == nil {
			conn.Close()
		}
		//connPool.numOpened--
		connPool.poolFacade.decrementNumOpened()
		return
	}

	if err != nil {
		//connPool.numOpened--
		connPool.poolFacade.decrementNumOpened()
		//TODO почему вызывается с nil?
		connPool.putConnectionConnPoolLocked(nil, err)

		connPool.mu.Unlock()
		connPool.poolFacade.maybeOpenNewConnections()
		return
	}

	dc := &driverConn{
		connPool:   connPool,
		createdAt:  nowFunc(),
		returnedAt: nowFunc(),
		ci:         conn,
	}

	if connPool.putConnectionConnPoolLocked(dc, err) {
		log.Println("ConnPool openNewConnection putConnectionConnPoolLocked success")
		connPool.addDepLocked(dc, dc)
	} else {
		log.Println("ConnPool openNewConnection putConnectionConnPoolLocked failure")
		//connPool.numOpened--
		connPool.poolFacade.decrementNumOpened()
		conn.Close()
	}
}

//TODO если можно достёт соединение из кеша
//     если можно создаёт новое соединение
//    		иначе делает запрос на получение соединения
// conn returns a newly-opened or cached *driverConn.
func (connPool *ConnPool) conn(ctx context.Context, strategy connReuseStrategy) (*driverConn, error) {
	log.Print("ConnPool conn")
	connPool.mu.Lock()

	// Check if the context is closed.
	if connPool.closed {
		log.Print("ConnPool conn is closed")
		connPool.mu.Unlock()
		return nil, errDBClosed
	} else {
		log.Print("ConnPool conn is not closed")
	}

	// Check if the context is expired.
	select {
	default:
		log.Print("ConnPool conn is not expired")
	case <-ctx.Done():
		log.Print("ConnPool conn is expired")
		connPool.mu.Unlock()
		return nil, ctx.Err()
	}
	lifetime := connPool.maxLifetime

	// Prefer a free connection, if possible.
	numFree := len(connPool.freeConn)
	canUseIdleConnection := strategy == cachedOrNewConn && 0 < numFree
	if canUseIdleConnection {
		log.Print("ConnPool conn canUseIdleConnection")

		var conn *driverConn
		//remove idle connection from slice
		for dc := range connPool.freeConn {
			conn = dc
		}

		delete(connPool.freeConn, conn)

		if conn == nil {
			panic("Невозможно")
		}
		//conn := connPool.freeConn.
		//copy(connPool.freeConn, connPool.freeConn[1:])
		//connPool.freeConn = connPool.freeConn[:numFree-1]

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
	} else {
		log.Print("ConnPool conn canUseIdleConnection not")
	}

	connPool.mu.Unlock()

	if !connPool.poolFacade.canAddNewConn() {
		log.Print("ConnPool conn !canAddNewConn")
		// TODO try to remove idle connection from another connection pool and
		//  wait for free connection

		// Make the connCreationResponse channel. It's buffered so that the
		// connectionOpener doesn't block while waiting for the responseChan to be read.

		responseChan := make(chan connCreationResponse, 1)

		connPool.mu.Lock()
		reqKey := connPool.nextRequestKeyLocked()
		//connPool.waitCount++
		connPool.connRequests[reqKey] = responseChan
		connPool.mu.Unlock()

		//TODO изолировать
		connPool.poolFacade.mu.Lock()
		connPool.poolFacade.waitCount++
		connPool.poolFacade.mu.Unlock()

		//
		//defer func() {
		//	log.Println("strange defer")
		//	connPool.mu.Lock()
		//	connPool.waitCount--
		//	connPool.mu.Unlock()
		//}()

		waitStart := nowFunc()

		// Timeout the connection request with the context.
		select {
		//Context expired
		case <-ctx.Done():
			// Remove the connection request and ensure no value has been sent
			// on it after removing.
			connPool.mu.Lock()
			delete(connPool.connRequests, reqKey)
			connPool.mu.Unlock()

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
		//created new connection
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
	} else {
		log.Print("ConnPool conn not canAddNewConn")
	}

	connPool.poolFacade.incrementNumOpened() // optimistically
	//connPool.numOpened++

	//Создание нового соединения
	log.Print("ConnPool conn creating a new pool")
	ci, err := connPool.connector.Connect(ctx)

	if err != nil {
		log.Printf("ConnPool conn creating a new pool with error %s", err.Error())
		connPool.poolFacade.decrementNumOpened()
		//connPool.numOpened-- // correct for earlier optimism
		//TODO попытка открыть новые соединения
		connPool.poolFacade.maybeOpenNewConnections()
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

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (connPool *ConnPool) nextRequestKeyLocked() uint64 {
	next := connPool.nextRequest
	connPool.nextRequest++
	return next
}

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

// maxBadConnRetries is the number of maximum retries if the driver returns
// driver.ErrBadConn to signal a broken connection before forcing a new
// connection to be opened.
const maxBadConnRetries = 2

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
	for dc := range connPool.freeConn {
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

//
//func (connPool *ConnPool) maxIdleConnsLocked() int {
//	n := connPool.maxIdleCount
//	switch {
//	case n == 0:
//		// TODO(bradfitz): ask driver, if supported, for its default preference
//		return defaultMaxIdleConns
//	case n < 0:
//		return 0
//	default:
//		return n
//	}
//}

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

func (connPool *ConnPool) removeConnFromFree(dc *driverConn) bool {
	_, ok := connPool.freeConn[dc]
	return ok
}

func (connPool *ConnPool) getWaitCount() int {
	connPool.mu.Lock()
	result := len(connPool.connRequests)
	connPool.mu.Unlock()
	return result
}
