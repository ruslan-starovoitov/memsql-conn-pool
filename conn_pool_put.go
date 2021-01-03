package cpool

import (
	"cpool/driver"
	"fmt"
)

// putConnHook is a hook for testing.
var putConnHook func(*ConnPool, *driverConn)

// debugGetPut determines whether getConn & putConn calls' stack traces
// are returned for more verbose crashes.
const debugGetPut = false

// TODO изучить / кладёт соединение в пулл / вызывает создание новых соединений
//  вызывается когда соединение соединение было создано, но контекст просрочился
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
		connPool.poolFacade.maybeOpenNewConnections()
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

// TODO выяснить когда это вызывается
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

	//TODO что это за ужас?
	numberOfRequests := len(connPool.poolFacade.connRequests)
	if numberOfRequests > 0 {
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
