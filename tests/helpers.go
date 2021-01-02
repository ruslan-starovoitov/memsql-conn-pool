package cpool_tests

import (
	"cpool"
	"fmt"
	"sync"
	"time"
)

//TODO add comments

func execSleepWait(delay time.Duration, credentials cpool.Credentials, wg *sync.WaitGroup, pm *cpool.PoolFacade) {
	execSleep(delay, credentials, pm)
	wg.Done()
}

func execSleep(delay time.Duration, credentials cpool.Credentials, pm *cpool.PoolFacade) {
	_, err := pm.Exec(credentials, fmt.Sprintf("select sleep(%v)", delay.Seconds()))
	if err != nil {
		panic(err)
	}
}

func querySleepWait(delay time.Duration, credentials cpool.Credentials, wg *sync.WaitGroup, poolFacade *cpool.PoolFacade) {
	querySleep(delay, credentials, poolFacade)
	wg.Done()
}

func querySleep(delay time.Duration, credentials cpool.Credentials, pm *cpool.PoolFacade) {
	rows, err := pm.Query(credentials, fmt.Sprintf("select sleep(%v)", delay.Seconds()))
	if err != nil {
		panic(err)
	}
	errCloseRows := rows.Close()
	if errCloseRows != nil {
		panic(errCloseRows)
	}
}

func queryRowSleepWait(delay time.Duration, credentials cpool.Credentials, wg *sync.WaitGroup, poolFacade *cpool.PoolFacade) {
	queryRowSleep(delay, credentials, poolFacade)
	wg.Done()
}

func queryRowSleep(delay time.Duration, credentials cpool.Credentials, pm *cpool.PoolFacade) {
	row, err := pm.QueryRow(credentials, fmt.Sprintf("select sleep(%v)", delay.Seconds()))
	if err != nil {
		panic(err)
	}

	var num int
	errScan := row.Scan(&num)
	if errScan != nil {
		panic(errScan)
	}
}

// waitTimeout waits for the waitgroup for the specified max timeout.
// Returns true if waiting timed out.
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})

	fun := func() {
		defer close(c)
		wg.Wait()
	}

	go fun()

	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}
