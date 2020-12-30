package memsql_conn_pool

func (pf *PoolFacade) incrementNumOpenedLocked() {
	pf.mu.Lock()
	pf.numOpen++
	pf.mu.Unlock()
}

func (pf *PoolFacade) decrementNumOpenedLocked() {
	pf.mu.Lock()
	pf.numOpen--
	pf.mu.Unlock()
}

//Stats TODO возвращает статистику текущего пула
func (pf *PoolFacade) Stats() PoolFacadeStats {
	pf.mu.Lock()

	//TODO заменить на счетчик
	numIdle := 0
	for tuple := range pf.pools.IterBuffered() {
		connPool := tuple.Val.(*ConnPool)
		connPool.mu.Lock()
		numIdle += len(connPool.freeConn)
		connPool.mu.Unlock()
	}

	result := PoolFacadeStats{
		NumIdle:       numIdle,
		NumOpen:       pf.numOpen,
		TotalMax:      pf.totalMax,
		NumUniqueDSNs: pf.pools.Count(),
	}
	pf.mu.Unlock()
	return result
}

//PoolFacadeStats TODO описывает статистику пула
type PoolFacadeStats struct {
	NumIdle       int
	NumOpen       int
	TotalMax      int
	NumUniqueDSNs int
}
