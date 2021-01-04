package cpool

import (
	"log"
	"strconv"
)

func (pf *PoolFacade) incrementNumOpened() {
	log.Println("PoolFacade incrementNumOpened start")
	pf.mu.Lock()
	pf.numOpen++
	pf.mu.Unlock()
	log.Println("PoolFacade incrementNumOpened end")
}

func (pf *PoolFacade) decrementNumOpened() {
	log.Println("PoolFacade decrementNumOpened start")
	pf.mu.Lock()
	pf.numOpen--
	pf.mu.Unlock()
	log.Println("PoolFacade decrementNumOpened end")
}

//Stats TODO возвращает статистику текущего пула
func (pf *PoolFacade) Stats() PoolFacadeStats {
	log.Println("stats request start")

	//TODO заменить на счетчик
	numIdle := 0
	index := 0

	log.Println("stats iterating through pools")
	for tuple := range pf.pools.IterBuffered() {
		index++
		connPool := tuple.Val.(*ConnPool)

		log.Printf("stats iterating through pools index=%v\n", index)

		log.Println("before lock")
		//connPool.mu.state
		connPool.mu.Lock()
		log.Println("lock")
		numIdle += len(connPool.freeConn)
		connPool.mu.Unlock()
		log.Println("unlock")

	}
	log.Println("stats after loop")

	pf.mu.Lock()

	result := PoolFacadeStats{
		NumIdle:       numIdle,
		NumOpen:       pf.numOpen,
		TotalMax:      pf.totalMax,
		NumUniqueDSNs: pf.pools.Count(),
	}
	pf.mu.Unlock()

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
