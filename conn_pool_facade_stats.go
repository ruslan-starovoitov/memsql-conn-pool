package cpool

import (
	"log"
	"strconv"
)

func (connPoolFacade *ConnPoolFacade) incrementNumOpened() {
	log.Println("ConnPoolFacade incrementNumOpened start")
	connPoolFacade.mu.Lock()
	connPoolFacade.numOpened++
	connPoolFacade.mu.Unlock()
	log.Println("ConnPoolFacade incrementNumOpened end")
}

func (connPoolFacade *ConnPoolFacade) decrementNumOpened() {
	log.Println("ConnPoolFacade decrementNumOpened start")
	connPoolFacade.mu.Lock()
	connPoolFacade.numOpened--
	connPoolFacade.mu.Unlock()
	log.Println("ConnPoolFacade decrementNumOpened end")
}

//Stats TODO возвращает статистику текущего пула
func (connPoolFacade *ConnPoolFacade) Stats() PoolFacadeStats {
	log.Println("stats request start")

	//TODO заменить на счетчик
	numIdle := 0
	index := 0

	log.Println("stats iterating through pools")
	for tuple := range connPoolFacade.pools.IterBuffered() {
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

	connPoolFacade.mu.Lock()

	result := PoolFacadeStats{
		NumIdle:       numIdle,
		NumOpen:       connPoolFacade.numOpened,
		TotalMax:      connPoolFacade.totalMax,
		NumUniqueDSNs: connPoolFacade.pools.Count(),
	}
	connPoolFacade.mu.Unlock()

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

func (connPoolFacade *ConnPoolFacade) StatsOfAllPools() []ConnPoolStats {
	countOfPools := connPoolFacade.pools.Count()
	countOfPoolsStr := strconv.Itoa(countOfPools)
	statsArray := make([]ConnPoolStats, 0)

	log.Print("Number of pools " + countOfPoolsStr)
	for tuple := range connPoolFacade.pools.IterBuffered() {
		log.Print("key is " + tuple.Key)
		connPool := tuple.Val.(*ConnPool)
		stats := connPool.Stats()
		statsArray = append(statsArray, stats)
	}

	return statsArray
}
