package cpool

import (
	"context"
	"log"
)

//TODO создаёт новое соединение в контексте без проверок ограничений
// копия connectionOpener
func (pf *PoolFacade) connectionOpener(ctx context.Context) {
	//ждём пока контекст закроется или попросят открыть новое соединение
	for {
		select {
		case <-ctx.Done():
			log.Println("PoolFacade connectionOpener ctx Done")
			return
		case connPool := <-pf.openerChannel:
			stats := pf.Stats()
			log.Printf("PoolFacade connectionOpener openerChannel stats = %v\n", stats)

			log.Println("")
			connPool.openNewConnection(ctx)
		}
	}
}

//TODO метод создаёт новое соединение/ вызывается из connectionOpener горутины
// Open one new connection
func (connPool *ConnPool) openNewConnection(ctx context.Context) {
	log.Println("ConnPool openNewConnection")

	// maybeOpenNewConnections has already executed connPool.numOpen++ before it sent
	// on connPool.openerCh. This function must execute connPool.numOpen-- if the
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
		//connPool.numOpen--
		connPool.poolFacade.decrementNumOpened()
		return
	}

	if err != nil {
		//connPool.numOpen--
		connPool.poolFacade.decrementNumOpened()
		//TODO почему вызывается с nil?
		connPool.putConnectionConnPoolLocked(nil, err)
		//TODO странно / попытка открыть новые соединения
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
		//connPool.numOpen--
		connPool.poolFacade.decrementNumOpened()
		conn.Close()
	}
}

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (pf *PoolFacade) nextRequestKeyLocked() uint64 {
	next := pf.nextRequest
	pf.nextRequest++
	return next
}

// TODO Вызывается при запросе нового канала/сохранения канала в список свободных/закрытии соединения
// Assumes poolFacade.mu is locked.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (pf *PoolFacade) maybeOpenNewConnections() {
	log.Println("maybeOpenNewConnections")
	pf.mu.Lock()
	defer pf.mu.Unlock()
	numRequests := len(pf.connRequests)

	//Существует ограничение
	if pf.isLimitExists() {
		numCanOpen := pf.totalMax - pf.numOpen
		if numCanOpen < numRequests {
			numRequests = numCanOpen
		}
	}

	for _, value := range pf.connRequests {
		pf.numOpen++ // optimistically
		if pf.closed {
			return
		}

		//Дать команду создать соединение для конкретного пула
		log.Println("maybeOpenNewConnections before openerChannel ")
		pf.openerChannel <- value.connPool
		log.Println("maybeOpenNewConnections after openerChannel ")

		numRequests--
		if numRequests == 0 {
			break
		}
	}

}
