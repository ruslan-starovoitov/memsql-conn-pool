package memsql_conn_pool

import (
	"context"
)

//TODO создаёт новое соединение в контексте без проверок ограничений
// копия connectionOpener
func (poolFacade *PoolFacade) globalConnectionOpener(ctx context.Context) {
	//ждём пока контекст закроется или попросят открыть новое соединение
	for {
		select {
		case <-ctx.Done():
			return
		case connPool := <-poolFacade.openerChannel:
			connPool.openNewConnection(ctx)
		}
	}
}

//TODO метод создаёт новое соединение/ вызывается из connectionOpener горутины
// Open one new connection
func (connPool *ConnPool) openNewConnection(ctx context.Context) {
	// maybeOpenNewConnectionsLocked has already executed connPool.numOpen++ before it sent
	// on connPool.openerCh. This function must execute connPool.numOpen-- if the
	// connection fails or is closed before returning.
	ci, err := connPool.connector.Connect(ctx)
	connPool.mu.Lock()
	defer connPool.mu.Unlock()
	if connPool.closed {
		if err == nil {
			ci.Close()
		}
		connPool.numOpen--
		return
	}
	if err != nil {
		connPool.numOpen--
		connPool.putConnDBLocked(nil, err)
		//TODO странно / попытка открыть новые соединения
		connPool.poolFacade.maybeOpenNewConnectionsLocked()
		return
	}
	dc := &driverConn{
		connPool:   connPool,
		createdAt:  nowFunc(),
		returnedAt: nowFunc(),
		ci:         ci,
	}
	if connPool.putConnDBLocked(dc, err) {
		connPool.addDepLocked(dc, dc)
	} else {
		connPool.numOpen--
		ci.Close()
	}
}

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (poolFacade *PoolFacade) nextRequestKeyLocked() uint64 {
	next := poolFacade.nextRequest
	poolFacade.nextRequest++
	return next
}

//TODO проблема с мьютексами
// Вызывается запросе нового канала/сохранения канала в список свободных/закрытии канала из sql.go
// Assumes poolFacade.mu is locked.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (poolFacade *PoolFacade) maybeOpenNewConnectionsLocked() {
	poolFacade.mu.Lock()
	numRequests := len(poolFacade.connRequests)

	if poolFacade.totalMax > 0 {
		numCanOpen := poolFacade.totalMax - poolFacade.numOpen
		if numCanOpen < numRequests {
			numRequests = numCanOpen
		}
	}

	for _, value := range poolFacade.connRequests {
		poolFacade.numOpen++ // optimistically
		if poolFacade.closed {
			return
		}

		//Дать команду создать соединение для конкретного пула
		poolFacade.openerChannel <- value.connPool

		numRequests--
		if numRequests == 0 {
			break
		}
	}

	poolFacade.mu.Unlock()
}
