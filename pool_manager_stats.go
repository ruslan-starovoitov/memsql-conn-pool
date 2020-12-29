package memsql_conn_pool

//Stats TODO возвращает статистику текущего пула
func (poolFacade *PoolFacade) Stats() PoolFacadeStats {
	poolFacade.mu.Lock()

	result := PoolFacadeStats{
		NumIdle:  poolFacade.numIdle,
		NumOpen:  poolFacade.numOpen,
		TotalMax: poolFacade.totalMax,
	}
	poolFacade.mu.Unlock()
	return result
}

//PoolFacadeStats TODO описывает статистику пула
type PoolFacadeStats struct {
	NumIdle  int
	NumOpen  int
	TotalMax int
}
