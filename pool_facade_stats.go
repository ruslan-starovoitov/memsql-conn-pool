package cpool

import (
	"log"
	"strconv"
)

func (pf *PoolFacade) incrementNumOpened() {
	pf.mu.Lock()
	pf.numOpen++
	pf.mu.Unlock()
}

func (pf *PoolFacade) decrementNumOpened() {
	pf.mu.Lock()
	pf.numOpen--
	pf.mu.Unlock()
}

//Stats TODO возвращает статистику текущего пула
func (pf *PoolFacade) Stats() PoolFacadeStats {
	log.Println("stats request start")

	pf.mu.Lock()
	defer pf.mu.Unlock()

	//TODO заменить на счетчик
	numIdle := 0
	index := 0

	log.Println("stats iterating through pools")
	for tuple := range pf.pools.IterBuffered() {
		index++
		connPool := tuple.Val.(*ConnPool)

		log.Printf("stats iterating through pools index=%v\n", index)

		connPool.mu.Lock()

		log.Println("lock")
		numIdle += len(connPool.freeConn)
		connPool.mu.Unlock()
		log.Println("unlock")

	}
	log.Println("stats after loop")
	result := PoolFacadeStats{
		NumIdle:       numIdle,
		NumOpen:       pf.numOpen,
		TotalMax:      pf.totalMax,
		NumUniqueDSNs: pf.pools.Count(),
	}

	log.Println("stats request end")

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
