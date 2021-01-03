package cpool

// TODO заменил глобальным maybeOpenNewConnectionsLocked в poolFacade
// TODO просит создать запрошеное кол-во соединений у другой горутины
// TODO откуда вызывается?
// Assumes connPool.mu is locked.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
//func (connPool *ConnPool) maybeOpenNewConnectionsLocked() {
//	numRequests := len(connPool.connRequests)
//	if connPool.maxOpen > 0 {
//		numCanOpen := connPool.maxOpen - connPool.numOpen
//		if numCanOpen < numRequests {
//			numRequests = numCanOpen
//		}
//	}
//	for numRequests > 0 {
//		connPool.numOpen++ // optimistically
//		numRequests--
//		if connPool.closed {
//			return
//		}
//		connPool.openerCh <- struct{}{}
//	}
//}

//TODO я заменил на globalConnectionOpener
//// Runs in a separate goroutine, opens new connections when requested.
//// TODO изучить/ запускается параллелльно с созданием нового пула
//func (connPool *ConnPool) connectionOpener(ctx context.Context) {
//	//ждём пока контекст закроется или попросят открыть новое соединение
//	for {
//		select {
//		case <-ctx.Done():
//			return
//		case <-connPool.openerCh:
//			connPool.openNewConnection(ctx)
//		}
//	}
//}
