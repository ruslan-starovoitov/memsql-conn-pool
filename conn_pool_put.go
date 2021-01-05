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
const debugGetPut = false

// putConn adds a connection to the connPool's free pool.
// err is optionally the last error that occurred on this connection.
func (connPool *ConnPool) putConn(dc *driverConn, err error, resetSession bool) {
	log.Println("ConnPool putConn")
	if err != driver.ErrBadConn {
		if !dc.validateConnection(resetSession) {
			err = driver.ErrBadConn
		}
	}

	connPool.poolFacade.mu.Lock()

	if !dc.inUse {
		log.Println("ConnPool putConn not dc.inUse")
		connPool.poolFacade.mu.Unlock()
		if debugGetPut {
			fmt.Printf("putConn(%v) DUPLICATE was: %s\n\nPREVIOUS was: %s", dc, stack(), connPool.lastPut[dc])
		}
		panic("sql: connection returned that was never out")
	} else {
		log.Println("ConnPool putConn dc.inUse ok")
	}

	if err != driver.ErrBadConn && dc.expired(connPool.maxLifetime) {
		log.Println("ConnPool putConn closing connection due to expiration")
		connPool.maxLifetimeClosed++
		err = driver.ErrBadConn
	}

	if debugGetPut {
		connPool.lastPut[dc] = stack()
	}
	dc.inUse = false
	dc.returnedAt = nowFunc()

	for _, fn := range dc.onPut {
		log.Println("ConnPool putConn call on put handlers")
		fn()
	}
	dc.onPut = nil

	if err == driver.ErrBadConn {
		// Don't reuse bad connections.
		// Since the conn is considered bad and is being discarded, treat it
		// as closed. Don't decrement the open count here, finalClose will
		// take care of that.
		connPool.poolFacade.maybeOpenNewConnections()
		connPool.poolFacade.mu.Unlock()
		dc.Close()
		return
	}

	if putConnHook != nil {
		log.Println("putConnHook is not nil")
		putConnHook(connPool, dc)
	}
	added := connPool.putConnectionConnPoolLocked(dc, nil)
	connPool.poolFacade.mu.Unlock()

	if !added {
		log.Println("warning ConnPool putConn connection is not added to free connections")
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
		log.Println("connPool putConnectionConnPoolLocked connection pool closed")
		return false
	}

	//TODO опасно
	//if !connPool.poolFacade.canAddNewConn() {
	//	log.Println("warning connPool putConnectionConnPoolLocked canAddNewConn")
	//	return false
	//}

	if connPool.isFreeConnectionsNeededLocked() {
		return connPool.returnConnectionOnRequestLocked(dc, err)
	} else if err == nil && !connPool.closed {
		return connPool.tryToSaveConnectionAsIdleLocked(dc)
	}
	return false
}

func (connPool *ConnPool) isFreeConnectionsNeededLocked() bool {
	//if connPool.waitCount != len(connPool.connRequests){
	//	str := fmt.Sprintf("connPool.waitCount is %v connRequests is %v", connPool.waitCount, len(connPool.connRequests))
	//	panic(str)
	//}

	//return 0 < connPool.waitCount
	return 0 < len(connPool.connRequests)
}

func (connPool *ConnPool) returnConnectionOnRequestLocked(dc *driverConn, err error) bool {
	log.Println("conn pool returnConnectionOnRequestLocked")
	var requestKey uint64
	var requestChan chan connCreationResponse

	if len(connPool.connRequests) == 0 {
		panic("return connection to nil")
	}
	//get one request
	for key, value := range connPool.connRequests {
		requestKey = key
		requestChan = value
		break
	}

	if requestChan == nil {
		log.Printf("len of connPool.connRequests is %v\n", len(connPool.connRequests))
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

	if _, ok := connPool.freeConn[dc]; ok {
		log.Println("connection saved as idle success")
	} else {
		panic("can not save connection as idle")
	}

	connPool.startCleanerLocked()
	return true
}
