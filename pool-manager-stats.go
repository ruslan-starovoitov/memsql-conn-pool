package memsql_conn_pool

//Stats TODO возвращает статистику текущего пула
func (poolManager * PoolManager) Stats() PoolManagerStats {
	poolManager.mu.Lock()
	
	result:=PoolManagerStats{
		NumIdle: poolManager.numIdle,
		NumOpen: poolManager.numOpen,
		TotalMax: poolManager.totalMax,
	}
	poolManager.mu.Unlock()
	return result
}

//PoolManagerStats TODO описывает статистику пула 
type PoolManagerStats struct{
	NumIdle int
	NumOpen int
	TotalMax int
}
