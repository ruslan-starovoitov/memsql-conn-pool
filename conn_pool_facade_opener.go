package cpool

import (
	"log"
)

func (connPoolFacade *ConnPoolFacade) getNumCanOpenConnectionsLocked() int {
	return connPoolFacade.totalMax - connPoolFacade.numOpened
}

// TODO Даёт команду открыть новые соединения если нужно.
//  Если лимит превышен, то попытается закрыть idle соединения.
// 	вызывать без локов
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (connPoolFacade *ConnPoolFacade) maybeOpenNewConnections() {
	log.Println("connPoolFacade maybeOpenNewConnections")

	connPoolFacade.mu.Lock()
	defer connPoolFacade.mu.Unlock()

	log.Println("connPoolFacade maybeOpenNewConnections 1")

	//how many connections is needed to open?
	numConnectionsRequested := connPoolFacade.getNumOfConnRequestsLocked()
	numCanOpenConnections := connPoolFacade.getNumCanOpenConnectionsLocked()

	log.Println("connPoolFacade maybeOpenNewConnections 2")

	//try to close idle connections if needed
	isNeedToCloseIdleConns := numCanOpenConnections < numConnectionsRequested

	log.Printf("connPoolFacade maybeOpenNewConnections isNeedToCloseIdleConns %v\n", isNeedToCloseIdleConns)
	if isNeedToCloseIdleConns {
		numIdleConnToClose := numConnectionsRequested - numCanOpenConnections
		log.Printf("connPoolFacade maybeOpenNewConnections numIdleConnToClose %v\n", numIdleConnToClose)
		connPoolFacade.maybeCloseIdleConnLocked(numIdleConnToClose)
		//update num of can open
		numCanOpenConnections = connPoolFacade.getNumCanOpenConnectionsLocked()

		log.Printf("connPoolFacade maybeOpenNewConnections updated numCanOpenConnections %v\n", numIdleConnToClose)
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
