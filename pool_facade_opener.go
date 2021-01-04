package cpool

import (
	"context"
	"log"
)

//TODO создаёт новое соединение в контексте без проверок ограничений
// копия connectionOpener
func (poolFacade *PoolFacade) connectionOpener(ctx context.Context) {
	//ждём пока контекст закроется или попросят открыть новое соединение
	for {
		select {
		case <-ctx.Done():
			log.Println("PoolFacade connectionOpener ctx Done")
			return
		case connPool := <-poolFacade.openerChannel:
			stats := poolFacade.Stats()
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
func (poolFacade *PoolFacade) nextRequestKeyLocked() uint64 {
	next := poolFacade.nextRequest
	poolFacade.nextRequest++
	return next
}

// TODO Вызывается при запросе нового канала/сохранения канала в список свободных/закрытии соединения
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (poolFacade *PoolFacade) maybeOpenNewConnections() {
	log.Println("maybeOpenNewConnections")
	poolFacade.mu.Lock()
	defer poolFacade.mu.Unlock()
	numRequests := len(poolFacade.connRequests)
	allowedNumOfNewConnections := numRequests

	//Существует ограничение
	if poolFacade.isConnectionLimitExistsLocked() {
		numCanOpen := poolFacade.totalMax - poolFacade.numOpen
		if numCanOpen < numRequests {
			allowedNumOfNewConnections = numCanOpen
		}
	}

	//Get connPools from map
	var connPoolsToCreateNewConnections []*ConnPool
	for _, value := range poolFacade.connRequests {
		allowedNumOfNewConnections--
		if allowedNumOfNewConnections < 0 {
			break
		}
		connPoolsToCreateNewConnections = append(connPoolsToCreateNewConnections, value.connPool)
	}

	//Give a command to open a connection
	for _, connPool := range connPoolsToCreateNewConnections {
		poolFacade.numOpen++ // optimistically
		if poolFacade.closed {
			return
		}

		log.Println("maybeOpenNewConnections before openerChannel ")
		poolFacade.openerChannel <- connPool
		log.Println("maybeOpenNewConnections after openerChannel ")
	}
}
