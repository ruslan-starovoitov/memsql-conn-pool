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

func (connPoolFacade *ConnPoolFacade) getNumCanOpenConnectionsLocked() int {
	return connPoolFacade.totalMax - connPoolFacade.numOpened
}

// TODO Даёт команду открыть новые соединения если нужно.
//  Если лимит превышен, то попытается закрыть idle соединения.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (connPoolFacade *ConnPoolFacade) maybeOpenNewConnections() {
	log.Println("maybeOpenNewConnections")

	connPoolFacade.mu.Lock()
	defer connPoolFacade.mu.Unlock()

	log.Println("maybeOpenNewConnections 1")

	//how many connections is needed to open?
	numConnectionsRequested := connPoolFacade.getNumOfConnRequestsLocked()
	numCanOpenConnections := connPoolFacade.getNumCanOpenConnectionsLocked()

	log.Println("maybeOpenNewConnections 2")

	//try to close idle connections if needed
	isNeedToCloseIdleConns := numCanOpenConnections < numConnectionsRequested
	if isNeedToCloseIdleConns {
		numIdleConnToClose := numConnectionsRequested - numCanOpenConnections
		connPoolFacade.maybeCloseIdleConn(numIdleConnToClose)
		//update num of can open
		numCanOpenConnections = connPoolFacade.getNumCanOpenConnectionsLocked()
	}

	log.Println("maybeOpenNewConnections 3")

	//calculate max num of connections to open
	numOfNewConnections := min(numConnectionsRequested, numCanOpenConnections)
	connPoolsToCreateNewConnections := connPoolFacade.getConnPoolsThatHaveRequestedNewConnections(numOfNewConnections)

	log.Println("maybeOpenNewConnections 4")
	//open new connections
	connPoolFacade.giveCommandToOpenNewConnections(connPoolsToCreateNewConnections)

	log.Println("maybeOpenNewConnections 5")
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
