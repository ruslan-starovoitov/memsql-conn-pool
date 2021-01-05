package cpool_tests

import (
	"cpool"
	"fmt"
	"sync"
	"time"
)

//TODO add comments

func execSleepWait(delay time.Duration, credentials cpool.Credentials, wg *sync.WaitGroup, poolFacade *cpool.ConnPoolFacade) {
	execSleep(delay, credentials, poolFacade)
	wg.Done()
}

func execSleep(delay time.Duration, credentials cpool.Credentials, poolFacade *cpool.ConnPoolFacade) {
	_, err := poolFacade.Exec(credentials, fmt.Sprintf("select sleep(%v)", delay.Seconds()))
	if err != nil {
		panic(err)
	}
}

func querySleepWait(delay time.Duration, credentials cpool.Credentials, wg *sync.WaitGroup, poolFacade *cpool.ConnPoolFacade) {
	querySleep(delay, credentials, poolFacade)
	wg.Done()
}

func querySleep(delay time.Duration, credentials cpool.Credentials, poolFacade *cpool.ConnPoolFacade) {
	rows, err := poolFacade.Query(credentials, fmt.Sprintf("select sleep(%v)", delay.Seconds()))
	if err != nil {
		panic(err)
	}
	errCloseRows := rows.Close()
	if errCloseRows != nil {
		panic(errCloseRows)
	}
}

func queryRowSleepWait(delay time.Duration, credentials cpool.Credentials, wg *sync.WaitGroup, poolFacade *cpool.ConnPoolFacade) {
	queryRowSleep(delay, credentials, poolFacade)
	wg.Done()
}

func queryRowSleep(delay time.Duration, credentials cpool.Credentials, poolFacade *cpool.ConnPoolFacade) {
	row, err := poolFacade.QueryRow(credentials, fmt.Sprintf("select sleep(%v)", delay.Seconds()))
	if err != nil {
		panic(err)
	}

	var num int
	errScan := row.Scan(&num)
	if errScan != nil {
		panic(errScan)
	}
}

// waitTimeout ждет выполнения wait group. Если выполнения заняло меньше timeout, то вернет true
// Если выполнение не закончилось за timeout, то вернет false
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) (ok bool, duration time.Duration) {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()

	startTime := time.Now()

	select {
	// completed normally
	case <-c:
		return true, time.Since(startTime)
		// timed out
	case <-time.After(timeout):
		return false, time.Since(startTime)
	}
}
