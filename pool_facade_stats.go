package cpool

import (
	"log"
	"strconv"
)

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

func (pf *PoolFacade) StatsOfAllPools() []ConnPoolStats {
	pf.mu.Lock()
	countOfPools := pf.pools.Count()
	countOfPoolsStr := strconv.Itoa(countOfPools)
	statsArray := make([]ConnPoolStats, 0)

	log.Print("AAA/Number of pools " + countOfPoolsStr)
	for tuple := range pf.pools.IterBuffered() {
		log.Print("key is " + tuple.Key)
		connPool := tuple.Val.(*ConnPool)
		stats := connPool.Stats()
		statsArray = append(statsArray, stats)
	}
	pf.mu.Unlock()
	return statsArray
}
