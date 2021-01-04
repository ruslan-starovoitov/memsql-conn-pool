package cpool

import (
	"context"
	"log"
)

//TODO слушает команды на создание новых соединений
//Runs in a separate goroutine, opens new connections when requested.
func (connPoolFacade *ConnPoolFacade) connectionOpener(ctx context.Context) {
	//ждём пока контекст закроется или попросят открыть новое соединение
	for {
		select {
		case <-ctx.Done():
			log.Println("ConnPoolFacade connectionOpener ctx Done")
			return
		//received a command to open a connection
		case connPool := <-connPoolFacade.openerChannel:
			stats := connPoolFacade.Stats()
			log.Printf("ConnPoolFacade connectionOpener openerChannel stats = %v\n", stats)
			connPool.openNewConnection(ctx)
		}
	}
}

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (connPoolFacade *ConnPoolFacade) nextRequestKeyLocked() uint64 {
	next := connPoolFacade.nextRequest
	connPoolFacade.nextRequest++
	return next
}

// TODO Даёт команду открыть новые соединения если нужно.
//  Если лимит превышен, то попытается закрыть idle соединения.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (connPoolFacade *ConnPoolFacade) maybeOpenNewConnections() {
	log.Println("maybeOpenNewConnections")

	connPoolFacade.mu.Lock()
	defer connPoolFacade.mu.Unlock()

	//how many connections is needed to open?
	numConnectionsRequested := connPoolFacade.getNumOfConnRequestsLocked()
	limit := connPoolFacade.totalMax
	numOpened := connPoolFacade.numOpened
	numCanOpenConnections := limit - numOpened

	//try to close idle connections if needed
	needToCloseIdleConns := numCanOpenConnections < numConnectionsRequested
	if needToCloseIdleConns {
		numIdleConnToClose := numConnectionsRequested - numCanOpenConnections
		numOfIdleConn := connPoolFacade.lruCache.Len()
		if numOfIdleConn < numIdleConnToClose {
			numIdleConnToClose = numOfIdleConn
		}
		connPoolFacade.closeLruIdleConnectionsLocked(numIdleConnToClose)
	}

	//calculate max num of connections to open
	numOfNewConnections := limit - connPoolFacade.numOpened
	connPoolsToCreateNewConnections := connPoolFacade.getConnPoolsThatHaveRequestedNewConnections(numOfNewConnections)

	//open new connections
	connPoolFacade.giveCommandToOpenNewConnections(connPoolsToCreateNewConnections)
}

//TODO даёт команды открыть соединение для каждого из пулов
func (connPoolFacade *ConnPoolFacade) giveCommandToOpenNewConnections(connPoolsToCreateNewConnections []*ConnPool) {
	for _, connPool := range connPoolsToCreateNewConnections {
		connPoolFacade.numOpened++ // optimistically
		if connPoolFacade.closed {
			return
		}

		log.Println("maybeOpenNewConnections before openerChannel ")
		connPoolFacade.openerChannel <- connPool
		log.Println("maybeOpenNewConnections after openerChannel ")
	}
}
