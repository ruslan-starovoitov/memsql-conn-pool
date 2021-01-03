package cpool

import (
	"context"
)

//TODO создаёт новое соединение в контексте без проверок ограничений
// копия connectionOpener
func (pf *PoolFacade) connectionOpener(ctx context.Context) {
	//ждём пока контекст закроется или попросят открыть новое соединение
	for {
		select {
		case <-ctx.Done():
			return
		case connPool := <-pf.openerChannel:
			connPool.openNewConnection(ctx)
		}
	}
}

//TODO метод создаёт новое соединение/ вызывается из connectionOpener горутины
// Open one new connection
func (connPool *ConnPool) openNewConnection(ctx context.Context) {
	// maybeOpenNewConnections has already executed connPool.numOpen++ before it sent
	// on connPool.openerCh. This function must execute connPool.numOpen-- if the
	// connection fails or is closed before returning.
	ci, err := connPool.connector.Connect(ctx)
	connPool.mu.Lock()
	defer connPool.mu.Unlock()
	if connPool.closed {
		if err == nil {
			ci.Close()
		}
		//connPool.numOpen--
		connPool.poolFacade.decrementNumOpened()
		return
	}
	if err != nil {
		//connPool.numOpen--
		connPool.poolFacade.decrementNumOpened()
		//TODO что это за ужас?
		// какого черта вызывается с nil?
		connPool.putConnectionConnPoolLocked(nil, err)
		//TODO странно / попытка открыть новые соединения
		connPool.poolFacade.maybeOpenNewConnections()
		return
	}
	dc := &driverConn{
		connPool:   connPool,
		createdAt:  nowFunc(),
		returnedAt: nowFunc(),
		ci:         ci,
	}
	if connPool.putConnectionConnPoolLocked(dc, err) {
		connPool.addDepLocked(dc, dc)
	} else {
		//connPool.numOpen--
		connPool.poolFacade.decrementNumOpened()
		ci.Close()
	}
}

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (pf *PoolFacade) nextRequestKeyLocked() uint64 {
	next := pf.nextRequest
	pf.nextRequest++
	return next
}

// Вызывается при запросе нового канала/сохранения канала в список свободных/закрытии соединения
// Assumes poolFacade.mu is locked.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (pf *PoolFacade) maybeOpenNewConnections() {
	pf.mu.Lock()
	numRequests := len(pf.connRequests)

	//Существует ограничение
	if pf.totalMax > 0 {
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
		pf.openerChannel <- value.connPool

		numRequests--
		if numRequests == 0 {
			break
		}
	}

	pf.mu.Unlock()
}
