package cpool_tests

import (
	"cpool"
	_ "cpool/mysql"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var user1Db1Credentials = cpool.Credentials{
	Username: "user1",
	Password: "pass1",
	Database: "hellomemsql1",
}
var user2Db1Credentials = cpool.Credentials{
	Username: "user2",
	Password: "pass2",
	Database: "hellomemsql1",
}
var user3Db1Credentials = cpool.Credentials{
	Username: "user3",
	Password: "pass3",
	Database: "hellomemsql1",
}
var user4Db2Credentials = cpool.Credentials{
	Username: "user4",
	Password: "pass4",
	Database: "hellomemsql2",
}
var user5Db2Credentials = cpool.Credentials{
	Username: "user5",
	Password: "pass5",
	Database: "hellomemsql2",
}
var user6Db2Credentials = cpool.Credentials{
	Username: "user6",
	Password: "pass6",
	Database: "hellomemsql2",
}
var credentials = []cpool.Credentials{
	user1Db1Credentials,
	user2Db1Credentials,
	user3Db1Credentials,
	user4Db2Credentials,
	user5Db2Credentials,
	user6Db2Credentials,
}

const (
	concurrencyLevel = 10
	idleTimeout      = time.Minute
)

func TestCrateDBConnect(t *testing.T) {
	t.Parallel()

	// Create pool
	pm := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)
	defer pm.Close()

	// Run simple query
	row, err := pm.QueryRow(user1Db1Credentials, "select 1+1")
	assert.NoError(t, err, "Unable to get connection")

	var result int
	err = row.Scan(&result)
	assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
	assert.Equal(t, 2, result, "bad result")
}

func TestConnectToDesiredDatabase(t *testing.T) {
	t.Parallel()

	// Create pool
	pm := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)
	defer pm.Close()

	for _, cr := range credentials {
		// Check database name
		row, err := pm.QueryRow(cr, "SELECT DATABASE();")
		assert.NoError(t, err, "connection error")

		var currentDB string
		err = row.Scan(&currentDB)
		assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
		assert.Equal(t, cr.Database, currentDB, "Did not connect to specified database ")

		// Check user name
		row, err = pm.QueryRow(cr, "select current_user")
		assert.NoError(t, err, "connection error")

		var user string
		err = row.Scan(&user)
		assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
		assert.Equal(t, cr.Username+"@%", user, "Did not connect as specified user")
	}
}

func TestPoolClosing(t *testing.T) {
	t.Parallel()

	// Create pool
	pm := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)

	// Check pool closing
	pm.Close()
	_, err := pm.QueryRow(user1Db1Credentials, "select 1+1")
	assert.Error(t, err, "Unable to close connection")
}

func TestExecFailure(t *testing.T) {
	t.Parallel()

	pm := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)
	defer pm.Close()

	_, err := pm.Exec(user1Db1Credentials, "incorrect sql statement;")
	assert.Error(t, err, "connection error")

	_, err = pm.Exec(user1Db1Credentials, "select 1;")
	assert.NoError(t, err, "Exec failure appears to have broken connection")
	pm.Close()
}

func TestExecFailureCloseBefore(t *testing.T) {
	t.Parallel()

	pm := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)
	pm.Close()

	_, err := pm.Exec(user1Db1Credentials, "select 1")
	require.Error(t, err)
}

//Проверка переиспользования одного соединения
func TestConnectionReuseInSequentialRequests(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		function func(delay time.Duration, cr cpool.Credentials, facade *cpool.PoolFacade)
	}{
		{"exec", execSleep},
		{"query", querySleep},
		{"queryRow", queryRowSleep},
		{"exec and query", func(delay time.Duration, cr cpool.Credentials, pf *cpool.PoolFacade) {
			execSleep(delay, cr, pf)
			querySleep(delay, cr, pf)
		}},
	}
	const connectionLimit = 100

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pm := cpool.NewPoolFacade("mysql", connectionLimit, time.Minute)
			defer pm.Close()

			//execute sequential requests
			for i := 0; i < 5; i++ {
				testCase.function(time.Second, user4Db2Credentials, pm)
			}

			time.Sleep(time.Second * 2)

			//check that only one connection was created
			stats := pm.Stats()
			assert.Equal(t, 1, stats.NumUniqueDSNs, "number of unique dsn")
			assert.Equal(t, connectionLimit, stats.TotalMax, "total max")
			assert.Equal(t, 1, stats.NumIdle, "num idle")
			assert.Equal(t, 0, stats.NumOpen, "num open")
		})
	}
	log.Print("log ended")
}

//проверка правильной статистики с одним соединением
func TestStatsOneConnectionExec(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		function func(delay time.Duration, cr cpool.Credentials, group *sync.WaitGroup, facade *cpool.PoolFacade)
	}{
		{"exec", execSleepWait},
		{"query", querySleepWait},
		{"queryRow", queryRowSleepWait},
		{"exec and query", func(delay time.Duration, cr cpool.Credentials, wg *sync.WaitGroup, pf *cpool.PoolFacade) {
			execSleepWait(delay, cr, wg, pf)
			querySleepWait(delay, cr, wg, pf)
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pm := cpool.NewPoolFacade("mysql", 1, idleTimeout)
			var wg sync.WaitGroup
			wg.Add(1)
			//run query to database
			go testCase.function(time.Second, user5Db2Credentials, &wg, pm)

			wg.Wait()

			//check pool facade stats
			stats := pm.Stats()
			log.Printf("%+v\n", stats)
			assert.Equal(t, 1, stats.NumUniqueDSNs, "number of unique dsn")
			assert.Equal(t, 1, stats.TotalMax, "total max")
			assert.Equal(t, 1, stats.NumIdle, "num idle")
			assert.Equal(t, 1, stats.NumOpen, "num open")

			//check pool stats
			allStats := pm.StatsOfAllPools()
			countOfPools := len(allStats)
			assert.Equal(t, 1, countOfPools, "too many pools")
			connPoolStats := allStats[0]
			assert.Equal(t, 1, connPoolStats.Idle)
			assert.Equal(t, 0, connPoolStats.InUse, "the pool was not released")
			assert.Equal(t, 0, connPoolStats.WaitCount)
		})
	}

}

//the pool will release idle connection if limit is exceeded
func TestNumberOfIdleConnections(t *testing.T) {
	t.Parallel()

	// create pool
	connectionLimit := 5
	pm := cpool.NewPoolFacade("mysql", connectionLimit, idleTimeout)

	defer pm.Close()

	var wg sync.WaitGroup
	// parallel requests
	for i := 0; i < connectionLimit; i++ {
		wg.Add(1)

		go execSleepWait(2, credentials[i], &wg, pm)
	}

	// wait
	wg.Wait()
	time.Sleep(time.Second * 5)
	// check open connections
	stats := pm.Stats()
	assert.Equal(t, connectionLimit, stats.NumUniqueDSNs)
	assert.Equal(t, connectionLimit, stats.TotalMax)
	assert.Equal(t, connectionLimit, stats.NumOpen)
	assert.Equal(t, connectionLimit, stats.NumIdle)
}

func TestReleaseIdleConnectionIfLimitExceeded(t *testing.T) {
	t.Parallel()

	// create pool with long idle timeout
	var wg sync.WaitGroup
	connectionLimit := 2
	pm := cpool.NewPoolFacade("mysql", connectionLimit, 5*time.Minute)
	defer pm.Close()

	queryExecutionDuration := 2 * time.Second
	//четверть секунды на накладные расходы на передачу данных по сети и работу БД
	queryTimeoutDuration := queryExecutionDuration + time.Second/4

	//заполнить пул запросами
	//make connectionLimit queries
	for i := 0; i < connectionLimit; i++ {
		wg.Add(1)
		go execSleepWait(queryExecutionDuration, user4Db2Credentials, &wg, pm)
	}

	//wait for their execution
	ok, duration := waitTimeout(&wg, queryTimeoutDuration)
	if !ok {
		assert.Fail(t, "To long query execution.")
	} else {
		log.Printf("first duration is %v\n", duration.Seconds())
	}

	stats := pm.Stats()
	assert.Equal(t, connectionLimit, stats.NumIdle)
	assert.Equal(t, connectionLimit, stats.NumOpen)

	//make another query with different credentials
	wg.Add(1)
	go execSleepWait(queryExecutionDuration, user1Db1Credentials, &wg, pm)

	//wait for new query
	ok, duration = waitTimeout(&wg, queryTimeoutDuration)
	if !ok {
		assert.Fail(t, "To long query execution. Probably pool doesn't support killing idle connections")
	} else {
		log.Printf("second duration is %v\n", duration.Seconds())
	}

	// check open connections
	stats = pm.Stats()
	assert.Equal(t, connectionLimit, stats.NumIdle)
}

func TestExceedingConnectionLimit(t *testing.T) {
	t.Parallel()

	// create pool with long idle timeout
	var wg sync.WaitGroup
	connectionLimit := 5
	pm := cpool.NewPoolFacade("mysql", connectionLimit, 5*time.Minute)
	defer pm.Close()

	queryExecutionDuration := 2 * time.Second
	//четверть секунды на накладные расходы на передачу данных по сети и работу БД
	//queryTimeoutDuration := queryExecutionDuration + time.Second/4

	//заполнить пул запросами
	//make connectionLimit*2 queries
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go execSleepWait(queryExecutionDuration, user4Db2Credentials, &wg, pm)
	}

	//wait for their execution
	startTime := time.Now()
	wg.Wait()
	duration := time.Since(startTime)
	log.Printf("duration is %v\n", duration.Seconds())
	//ok, duration:= waitTimeout(&wg, queryTimeoutDuration*2)
	//if !ok {
	//	assert.Failf(t, "To long query execution.",	"Duration is %v seconds.", duration.Seconds())
	//}else{
	//	log.Printf("duration is %v\n", duration.Seconds())
	//}

	// check open connections
	stats := pm.Stats()
	assert.Equal(t, connectionLimit, stats.NumIdle)
}

// the pool will release idle connection if limit
func TestConnectionLifetimeExceeded(t *testing.T) {
	t.Parallel()

	// create pool
	pm := cpool.NewPoolFacade("mysql", 1, time.Second)
	defer pm.Close()

	// exec
	_, err := pm.Exec(user5Db2Credentials, "select 1;")
	assert.NoError(t, err)

	// sleep for delete idle connection
	time.Sleep(2 * time.Second)

	// check that connection killed
	stats := pm.Stats()
	assert.Equal(t, 0, stats.NumIdle)
	assert.Equal(t, 0, stats.NumOpen)
	assert.Equal(t, 1, stats.TotalMax)
}

// the pool will not release idle connection if limit
func TestConnectionLifetimeNotExceeded(t *testing.T) {
	t.Parallel()

	// create pool
	pm := cpool.NewPoolFacade("mysql", 1, time.Second)
	defer pm.Close()

	// exec
	execSleep(time.Second, user5Db2Credentials, pm)

	// no sleep
	//time.Sleep(2*time.Second)

	// check that connection killed
	stats := pm.Stats()
	assert.Equal(t, 1, stats.NumIdle, "num idle")
	assert.Equal(t, 1, stats.NumOpen, "num open")
	assert.Equal(t, 1, stats.TotalMax, "total max")
}

func TestCreatePoolWithDifferentConnectionLimits(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		connectionLimit int
	}{
		{"Connection limit is lower that zero", -1},
		{"Connection limit is zero", 0},
		{"Connection limit is greater that zero", 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			//catch panic
			defer func() {
				errorHasOccurred := recover() != nil
				assert.False(t, errorHasOccurred, "Unexpected error.")
			}()

			_ = cpool.NewPoolFacade("mysql", testCase.connectionLimit, time.Second)
		})
	}
}
