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
	poolFacade := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)
	defer poolFacade.Close()

	// Run simple query
	row, err := poolFacade.QueryRow(user1Db1Credentials, "select 1+1")
	assert.NoError(t, err, "Unable to get connection")

	var result int
	err = row.Scan(&result)
	assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
	assert.Equal(t, 2, result, "bad result")
}

func TestConnectToDesiredDatabase(t *testing.T) {
	t.Parallel()

	// Create pool
	poolFacade := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)
	defer poolFacade.Close()

	for _, cr := range credentials {
		// Check database name
		row, err := poolFacade.QueryRow(cr, "SELECT DATABASE();")
		assert.NoError(t, err, "connection error")

		var currentDB string
		err = row.Scan(&currentDB)
		assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
		assert.Equal(t, cr.Database, currentDB, "Did not connect to specified database ")

		// Check user name
		row, err = poolFacade.QueryRow(cr, "select current_user")
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
	poolFacade := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)

	// Check pool closing
	poolFacade.Close()
	_, err := poolFacade.QueryRow(user1Db1Credentials, "select 1+1")
	assert.Error(t, err, "Unable to close connection")
}

func TestExecFailure(t *testing.T) {
	t.Parallel()

	poolFacade := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)
	defer poolFacade.Close()

	_, err := poolFacade.Exec(user1Db1Credentials, "incorrect sql statement;")
	assert.Error(t, err, "connection error")

	_, err = poolFacade.Exec(user1Db1Credentials, "select 1;")
	assert.NoError(t, err, "Exec failure appears to have broken connection")
	poolFacade.Close()
}

func TestExecFailureCloseBefore(t *testing.T) {
	t.Parallel()

	poolFacade := cpool.NewPoolFacade("mysql", concurrencyLevel, idleTimeout)
	poolFacade.Close()

	_, err := poolFacade.Exec(user1Db1Credentials, "select 1")
	require.Error(t, err)
}

//Проверка переиспользования одного соединения
func TestConnectionReuseInSequentialRequests(t *testing.T) {
	t.Parallel()

	numberOfRepetitions := 1
	testCases := []struct {
		name     string
		function func(delay time.Duration, cr cpool.Credentials, facade *cpool.ConnPoolFacade)
	}{
		{"exec", execSleep},
		{"query", querySleep},
		{"queryRow", queryRowSleep},
		//{"exec and query", func(delay time.Duration, cr cpool.Credentials, pf *cpool.ConnPoolFacade) {
		//	execSleep(delay, cr, pf)
		//	querySleep(delay, cr, pf)
		//}},
	}
	const connectionLimit = 100

	for _, testCase := range testCases {
		for i := 0; i < numberOfRepetitions; i++ {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()
				poolFacade := cpool.NewPoolFacade("mysql", connectionLimit, time.Minute)
				defer poolFacade.Close()

				//execute sequential requests
				for i := 0; i < 5; i++ {
					testCase.function(time.Second, user4Db2Credentials, poolFacade)
				}

				time.Sleep(time.Second * 2)

				//check that only one connection was created
				stats := poolFacade.Stats()
				assert.Equal(t, 1, stats.NumUniqueDSNs, "number of unique dsn")
				assert.Equal(t, connectionLimit, stats.TotalMax, "total max")
				assert.Equal(t, 1, stats.NumIdle, "num idle")
				assert.Equal(t, 0, stats.NumOpen, "num open")
			})
		}
	}
	log.Print("log ended")
}

//проверка правильной статистики с одним соединением
func TestStatsOneConnectionExec(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		function func(delay time.Duration, cr cpool.Credentials, group *sync.WaitGroup, facade *cpool.ConnPoolFacade)
	}{
		{"exec", execSleepWait},
		{"query", querySleepWait},
		{"queryRow", queryRowSleepWait},
		{"exec and query", func(delay time.Duration, cr cpool.Credentials, wg *sync.WaitGroup, pf *cpool.ConnPoolFacade) {
			wg.Add(1)
			execSleepWait(delay, cr, wg, pf)
			querySleepWait(delay, cr, wg, pf)
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			poolFacade := cpool.NewPoolFacade("mysql", 1, idleTimeout)
			var wg sync.WaitGroup
			wg.Add(1)
			//run query to database
			go testCase.function(time.Second, user5Db2Credentials, &wg, poolFacade)

			wg.Wait()

			//check pool facade stats
			stats := poolFacade.Stats()
			log.Printf("%+v\n", stats)
			assert.Equal(t, 1, stats.NumUniqueDSNs, "number of unique dsn")
			assert.Equal(t, 1, stats.TotalMax, "total max")
			assert.Equal(t, 1, stats.NumIdle, "num idle")
			assert.Equal(t, 1, stats.NumOpen, "num open")

			//check pool stats
			allStats := poolFacade.StatsOfAllPools()
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
	poolFacade := cpool.NewPoolFacade("mysql", connectionLimit, idleTimeout)

	defer poolFacade.Close()

	var wg sync.WaitGroup
	// parallel requests
	for i := 0; i < connectionLimit; i++ {
		wg.Add(1)

		go execSleepWait(2, credentials[i], &wg, poolFacade)
	}

	// wait
	wg.Wait()
	time.Sleep(time.Second * 5)
	// check open connections
	stats := poolFacade.Stats()
	assert.Equal(t, connectionLimit, stats.NumUniqueDSNs)
	assert.Equal(t, connectionLimit, stats.TotalMax)
	assert.Equal(t, connectionLimit, stats.NumOpen)
	assert.Equal(t, connectionLimit, stats.NumIdle)
}

func TestExceedingConnectionLimit(t *testing.T) {
	t.Parallel()

	// create pool with long idle timeout
	var wg sync.WaitGroup
	connectionLimit := 5
	numOfParallelGroups := 2
	parallelConnections := connectionLimit * numOfParallelGroups
	queryExecutionDuration := 2 * time.Second
	networkOverheadDuration := time.Second / 4
	queryTimeoutDuration := queryExecutionDuration + networkOverheadDuration
	groupsExecutionDuration := time.Duration(numOfParallelGroups) * queryTimeoutDuration

	log.Printf("groupsExecutionDuration = %v\n", groupsExecutionDuration.Seconds())

	poolFacade := cpool.NewPoolFacade("mysql", connectionLimit, 5*time.Minute)
	defer poolFacade.Close()

	//заполнить пул запросами
	for i := 0; i < parallelConnections; i++ {
		wg.Add(1)
		go execSleepWait(queryExecutionDuration, user4Db2Credentials, &wg, poolFacade)
	}

	//it is expected that the queries will be executed in two groups of 5
	ok, duration := waitTimeout(&wg, groupsExecutionDuration)
	assert.True(t, ok, "To long query execution.", "Duration is %v seconds.", duration.Seconds())

	// check open connections
	stats := poolFacade.Stats()
	assert.Equal(t, connectionLimit, stats.NumIdle)
	assert.Equal(t, 0, stats.NumOpen)

	time.Sleep(time.Second * 3)
	stats = poolFacade.Stats()
	assert.Equal(t, connectionLimit, stats.NumIdle)
	assert.Equal(t, 0, stats.NumOpen)
}

// the pool will release idle connection if limit
func TestConnectionLifetimeExceeded(t *testing.T) {
	t.Parallel()

	// create pool
	poolFacade := cpool.NewPoolFacade("mysql", 1, time.Second)
	defer poolFacade.Close()

	// exec
	_, err := poolFacade.Exec(user5Db2Credentials, "select 1;")
	assert.NoError(t, err)

	// sleep for delete idle connection
	time.Sleep(2 * time.Second)

	// check that connection killed
	stats := poolFacade.Stats()
	assert.Equal(t, 0, stats.NumIdle)
	assert.Equal(t, 0, stats.NumOpen)
	assert.Equal(t, 1, stats.TotalMax)
}

// the pool will not release idle connection if limit
func TestConnectionLifetimeNotExceeded(t *testing.T) {
	t.Parallel()

	// create pool
	poolFacade := cpool.NewPoolFacade("mysql", 1, time.Second)
	defer poolFacade.Close()

	// exec
	execSleep(time.Second, user5Db2Credentials, poolFacade)

	// no sleep
	//time.Sleep(2*time.Second)

	// check that connection killed
	stats := poolFacade.Stats()
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

//TODO deadlock
//при переиспользовании возвращается канал по нужным логину и паролю
func TestConnectToDesiredDatabaseReuse(t *testing.T) {
	t.Parallel()
	log.Println("TEST START")

	// Create pool
	connectionLimit := 1
	numRepetitions := 1
	poolFacade := cpool.NewPoolFacade("mysql", connectionLimit, idleTimeout)
	defer poolFacade.Close()

	for _, cr := range credentials {
		log.Printf("credentials %+v\n", cr)
		for i := 0; i < numRepetitions; i++ {
			// Check database name
			log.Println("111111111111111111111111111111111111111111")
			row, err := poolFacade.QueryRow(cr, "SELECT DATABASE();")
			require.NoError(t, err, "connection error")
			log.Println("111111111111111111111111111111111111111111")

			log.Println("22222222222222222222222222222222222222222")
			var currentDB string
			err = row.Scan(&currentDB)
			require.NoError(t, err, "QueryRow Scan unexpectedly failed")
			require.Equal(t, cr.Database, currentDB, "Did not connect to specified database ")
			log.Println("22222222222222222222222222222222222222222")

			log.Println("3333333333333333333333333333333333333333")
			// Check user name
			row, err = poolFacade.QueryRow(cr, "select current_user")
			require.NoError(t, err, "connection error")
			log.Println("3333333333333333333333333333333333333333")

			log.Println("44444444444444444444444444444444444444444")
			var user string
			err = row.Scan(&user)
			require.NoError(t, err, "QueryRow Scan unexpectedly failed")
			require.Equal(t, cr.Username+"@%", user, "Did not connect as specified user")
			log.Println("44444444444444444444444444444444444444444")
		}
	}
}

//проверить, что закроет старое простаивающее соединение
func TestConnPoolFacadeCloseIdleConnIfLimitExceeded(t *testing.T) {
	t.Parallel()

	// Create pool
	connectionLimit := 1
	poolFacade := cpool.NewPoolFacade("mysql", connectionLimit, idleTimeout)
	defer poolFacade.Close()

	for _, cr := range credentials {
		// Check database name
		row, err := poolFacade.QueryRow(cr, "SELECT DATABASE();")
		require.NoError(t, err, "connection error")

		var currentDB string
		err = row.Scan(&currentDB)
		require.NoError(t, err, "QueryRow Scan unexpectedly failed")
		require.Equal(t, cr.Database, currentDB, "Did not connect to specified database ")

		// Check user name
		row, err = poolFacade.QueryRow(cr, "select current_user")
		require.NoError(t, err, "connection error")

		var user string
		err = row.Scan(&user)
		require.NoError(t, err, "QueryRow Scan unexpectedly failed")
		require.Equal(t, cr.Username+"@%", user, "Did not connect as specified user")

		stats := poolFacade.Stats()
		log.Printf("%+v\n", stats)
		require.Equal(t, connectionLimit, stats.NumOpen)
	}
}

//проверить, что закроет старое простаивающее соединение
func TestSimpleConnectionStatsCheck(t *testing.T) {
	t.Parallel()

	log.Println("TEST START")

	// Create pool
	connectionLimit := 1
	poolFacade := cpool.NewPoolFacade("mysql", connectionLimit, idleTimeout)
	defer poolFacade.Close()

	stats := poolFacade.Stats()
	assert.Equal(t, 0, stats.NumOpen)
	assert.Equal(t, 0, stats.NumIdle)
	assert.Equal(t, 0, stats.NumUniqueDSNs)
	assert.Equal(t, connectionLimit, stats.TotalMax)

	execSleep(time.Second*2, user6Db2Credentials, poolFacade)

	stats = poolFacade.Stats()
	assert.Equal(t, 1, stats.NumOpen)
	assert.Equal(t, 1, stats.NumIdle)
	assert.Equal(t, 1, stats.NumUniqueDSNs)
	assert.Equal(t, connectionLimit, stats.TotalMax)

	allPoolsStats := poolFacade.StatsOfAllPools()
	log.Printf("%+v\n", allPoolsStats)
}

//проверить, что закроет старое простаивающее соединение
func TestConnectionReuse(t *testing.T) {
	t.Parallel()

	// Create pool
	connectionLimit := 1
	cr := user2Db1Credentials
	poolFacade := cpool.NewPoolFacade("mysql", connectionLimit, idleTimeout)
	defer poolFacade.Close()

	for i := 0; i < 100; i++ {
		// Check database name
		row, err := poolFacade.QueryRow(cr, "SELECT DATABASE();")
		require.NoError(t, err, "connection error")

		var currentDB string
		err = row.Scan(&currentDB)
		require.NoError(t, err, "QueryRow Scan unexpectedly failed")
		require.Equal(t, cr.Database, currentDB, "Did not connect to specified database ")

		// Check user name
		row, err = poolFacade.QueryRow(cr, "select current_user")
		require.NoError(t, err, "connection error")

		var user string
		err = row.Scan(&user)
		require.NoError(t, err, "QueryRow Scan unexpectedly failed")
		require.Equal(t, cr.Username+"@%", user, "Did not connect as specified user")

		stats := poolFacade.Stats()
		log.Printf("%+v\n", stats)
		require.Equal(t, connectionLimit, stats.NumOpen)

	}
}
