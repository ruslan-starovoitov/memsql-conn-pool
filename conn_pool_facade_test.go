package cpool

//
//import (
//	"fmt"
//	"github.com/stretchr/testify/assert"
//	"log"
//	"sync"
//	"testing"
//	"time"
//)
//
//var credentials = Credentials{
//	Username: "user1",
//	Password: "pass1",
//	Database: "hellomemsql1",
//}
//
//
//func TestExceedingConnectionLimit(t *testing.T) {
//	t.Parallel()
//
//	// create pool with long idle timeout
//	var wg sync.WaitGroup
//	connectionLimit := 5
//	parallelConnections := 10
//	poolFacade := NewPoolFacade("mysql", connectionLimit, 5*time.Minute)
//	defer poolFacade.Close()
//
//
//	queryExecutionDuration := 2 * time.Second
//	//четверть секунды на накладные расходы на передачу данных по сети и работу БД
//	//queryTimeoutDuration := queryExecutionDuration + time.Second/4
//
//	//заполнить пул запросами
//	for i := 0; i < parallelConnections; i++ {
//		wg.Add(1)
//		go execSleepWait(queryExecutionDuration, credentials, &wg, poolFacade)
//	}
//
//	//wait for their execution
//	startTime := time.Now()
//	wg.Wait()
//	duration := time.Since(startTime)
//	log.Printf("duration is %v\n", duration.Seconds())
//
//	// check open connections
//	stats := poolFacade.Stats()
//	assert.Equal(t, connectionLimit, stats.NumIdle)
//}
//
//func execSleepWait(delay time.Duration, credentials Credentials, wg *sync.WaitGroup, pm *ConnPoolFacade) {
//	execSleep(delay, credentials, pm)
//	wg.Done()
//}
//
//func execSleep(delay time.Duration, credentials Credentials, pm *ConnPoolFacade) {
//	_, err := pm.Exec(credentials, fmt.Sprintf("select sleep(%v)", delay.Seconds()))
//	if err != nil {
//		panic(err)
//	}
//}
