package cpool

import "log"

func (connPoolFacade *ConnPoolFacade) maybeCloseIdleConn(numIdleConnToClose int) {
	numOfIdleConn := connPoolFacade.lruCache.Len()
	log.Printf("num of idle conns = %v\n", numOfIdleConn)
	if numOfIdleConn < numIdleConnToClose {
		numIdleConnToClose = numOfIdleConn
	}
	connPoolFacade.closeLruIdleConnectionsLocked(numIdleConnToClose)
}

//TODO это можно запускать в отдельной горутине
//closeLruIdleConnectionsLocked пытается закрыть numConnToClose соединений
func (connPoolFacade *ConnPoolFacade) closeLruIdleConnectionsLocked(numConnToClose int) {
	for i := 0; i < numConnToClose; i++ {
		//Достать элемент из кеша
		key, _, ok := connPoolFacade.lruCache.RemoveOldest()
		if !ok {
			panic("Не удалось достать соединение из lru кеша")
		}

		//Привести к нужному типу данных
		dc, ok := key.(*driverConn)
		if !ok {
			panic("Неверный тип ключа.")
		}

		ok = dc.connPool.removeConnFromFree(dc)
		if !ok {
			panic("Не удалось удалить соединение из свободных")
		}

		//Закрыть соединение
		err := dc.Close()
		if err != nil {
			panic(err)
		}
	}
}

func (connPoolFacade *ConnPoolFacade) removeConnFromLru(dc *driverConn) {
	present := connPoolFacade.lruCache.Remove(dc)
	if !present {
		panic("lru does not contain connection")
	}
}

func (connPoolFacade *ConnPoolFacade) putConnToLru(dc *driverConn) {
	evicted := connPoolFacade.lruCache.Add(dc, nil)
	if evicted {
		panic("Удаления элемента из кеша не должно произойти. Кеш создавался максимального размера изначально. " +
			"Возможно, соединения не удаляются из кеша.")
	}
}
