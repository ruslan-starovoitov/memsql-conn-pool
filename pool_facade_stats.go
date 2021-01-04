package cpool

import (
	"log"
	"strconv"
)

func (poolFacade *PoolFacade) incrementNumOpened() {
	log.Println("PoolFacade incrementNumOpened start")
	poolFacade.mu.Lock()
	poolFacade.numOpen++
	poolFacade.mu.Unlock()
	log.Println("PoolFacade incrementNumOpened end")
}

func (poolFacade *PoolFacade) decrementNumOpened() {
	log.Println("PoolFacade decrementNumOpened start")
	poolFacade.mu.Lock()
	poolFacade.numOpen--
	poolFacade.mu.Unlock()
	log.Println("PoolFacade decrementNumOpened end")
}

//Stats TODO возвращает статистику текущего пула
func (poolFacade *PoolFacade) Stats() PoolFacadeStats {
	log.Println("stats request start")

	//TODO заменить на счетчик
	numIdle := 0
	index := 0

	log.Println("stats iterating through pools")
	for tuple := range poolFacade.pools.IterBuffered() {
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

	poolFacade.mu.Lock()

	result := PoolFacadeStats{
		NumIdle:       numIdle,
		NumOpen:       poolFacade.numOpen,
		TotalMax:      poolFacade.totalMax,
		NumUniqueDSNs: poolFacade.pools.Count(),
	}
	poolFacade.mu.Unlock()

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

func (poolFacade *PoolFacade) StatsOfAllPools() []ConnPoolStats {
	poolFacade.mu.Lock()
	countOfPools := poolFacade.pools.Count()
	countOfPoolsStr := strconv.Itoa(countOfPools)
	statsArray := make([]ConnPoolStats, 0)

	log.Print("AAA/Number of pools " + countOfPoolsStr)
	for tuple := range poolFacade.pools.IterBuffered() {
		log.Print("key is " + tuple.Key)
		connPool := tuple.Val.(*ConnPool)
		stats := connPool.Stats()
		statsArray = append(statsArray, stats)
	}
	poolFacade.mu.Unlock()
	return statsArray
}
