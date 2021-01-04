package cpool

import (
	"cpool/driver"
	"fmt"
	"log"
)

// putConnHook is a hook for testing.
var putConnHook func(*ConnPool, *driverConn)

// debugGetPut determines whether getConn & putConn calls' stack traces
// are returned for more verbose crashes.
//TODO replace with false
const debugGetPut = true

// TODO изучить / кладёт соединение в пулл / вызывает создание новых соединений
//  вызывается когда соединение соединение было создано, но контекст просрочился
// putConn adds a connection to the connPool's free pool.
// err is optionally the last error that occurred on this connection.
func (connPool *ConnPool) putConn(dc *driverConn, err error, resetSession bool) {
	log.Println("ConnPool putConn")
	if err != driver.ErrBadConn {
		if !dc.validateConnection(resetSession) {
			err = driver.ErrBadConn
		}
	}

	connPool.mu.Lock()

	if !dc.inUse {
		log.Println("ConnPool putConn not dc.inUse")
		connPool.mu.Unlock()
		if debugGetPut {
			fmt.Printf("putConn(%v) DUPLICATE was: %s\n\nPREVIOUS was: %s", dc, stack(), connPool.lastPut[dc])
		}
		panic("sql: connection returned that was never out")
	} else {
		log.Println("ConnPool putConn dc.inUse")
	}

	if err != driver.ErrBadConn && dc.expired(connPool.maxLifetime) {
		log.Println("closing connection due to expiration")
		connPool.maxLifetimeClosed++
		err = driver.ErrBadConn
	}

	if debugGetPut {
		connPool.lastPut[dc] = stack()
	}
	dc.inUse = false
	dc.returnedAt = nowFunc()

	for _, fn := range dc.onPut {
		log.Println("call on put handlers")
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
		log.Println("putConnHook is not nil")
		putConnHook(connPool, dc)
	}
	added := connPool.putConnectionConnPoolLocked(dc, nil)
	connPool.mu.Unlock()

	log.Println("ConnPool putConn 8")
	if !added {
		log.Println("ConnPool putConn 9")
		dc.Close()
		return
	}

	log.Println("ConnPool putConn 10")
}

// TODO выяснить когда это вызывается
// 	после освобождения соединения берется один запрос из словаря и ему отдаётся свободное соединение
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
	log.Println("connPool putConnectionConnPoolLocked")

	if connPool.closed {
		return false
	}
	if connPool.poolFacade.isOpenConnectionLimitExceeded() {
		log.Println("warning connPool putConnectionConnPoolLocked")
		return false
	}
	if connPool.isFreeConnectionsNeededLocked() {
		return connPool.returnConnectionOnRequestLocked(dc, err)
	} else if err == nil && !connPool.closed {
		return connPool.tryToSaveConnectionAsIdleLocked(dc)
	}
	return false
}

func (connPool *ConnPool) isFreeConnectionsNeededLocked() bool {
	return 0 < connPool.waitCount
}

func (connPool *ConnPool) returnConnectionOnRequestLocked(dc *driverConn, err error) bool {
	log.Println("conn pool returnConnectionOnRequestLocked")
	var requestKey uint64
	var requestChan chan connCreationResponse

	//get one request
	for key, value := range connPool.connRequests {
		requestKey = key
		requestChan = value
		break
	}

	if requestChan == nil {
		panic("requestChan is nil. probably there is no lock or request created in incorrect way")
		return false
	}

	delete(connPool.connRequests, requestKey) // Remove from pending requests.

	if err == nil {
		dc.inUse = true
	}

	//TODO в чем смысл сохранять соединения с ошибками?
	requestChan <- connCreationResponse{
		conn: dc,
		err:  err,
	}
	return true
}

func (connPool *ConnPool) tryToSaveConnectionAsIdleLocked(dc *driverConn) bool {
	log.Println("conn pool tryToSaveConnectionAsIdleLocked")

	connPool.poolFacade.putConnToLru(dc)
	if _, ok := connPool.freeConn[dc]; ok {
		panic("pool already contains such driver connection")
	}

	connPool.freeConn[dc] = struct{}{}
	connPool.startCleanerLocked()
	return true
}
