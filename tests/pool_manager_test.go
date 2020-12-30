package memsql_conn_pool_tests

import (
	cpool "memsql-conn-pool"
	_ "memsql-conn-pool/mysql"
	"strconv"
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

//проверка правильной статистики с одним соединением
//func TestStatsOneConnection(t *testing.T){
//	t.Parallel()
//	pm := cpool.NewPoolFacade("mysql", 1, idleTimeout)
//
//	var wg sync.WaitGroup
//	go execSleep(2, user5Db2Credentials, &wg, pm)
//
//	wg.Wait()
//
//	stats := pm.Stats()
//	assert.Equal(t, 1, stats.NumUniqueDSNs)
//	assert.Equal(t, 1, stats.TotalMax)
//	assert.Equal(t, 1, stats.NumIdle)
//	assert.Equal(t, 1, stats.NumOpen)
//}

// the pool will release idle connection if limit
//func TestNumberOfIdleConnections(t *testing.T) {
//	t.Parallel()
//
//	// create pool
//	connectionLimit := 5
//	pm := cpool.NewPoolFacade("mysql", connectionLimit, idleTimeout)
//
//	defer pm.Close()
//
//	var wg sync.WaitGroup
//	// parallel requests
//	for i := 0; i < connectionLimit; i++ {
//		wg.Add(1)
//
//		go execSleep(2, credentials[i], &wg, pm)
//	}
//
//	// wait
//	wg.Wait()
//	time.Sleep(time.Second * 5)
//	// check open connections
//	stats := pm.Stats()
//	assert.Equal(t, connectionLimit, stats.NumUniqueDSNs)
//	assert.Equal(t, connectionLimit, stats.TotalMax)
//	assert.Equal(t, connectionLimit, stats.NumOpen)
//	assert.Equal(t, connectionLimit, stats.NumIdle)
//}
//
//func TestReleaseIdleConnectionIfLimitExeeded(t *testing.T) {
//	t.Parallel()
//
//	// create pool
//	connectionLimit := 5
//	oneExecDurationSec := 2
//	expectedTotalDurationSec := 2 * oneExecDurationSec
//	pm := cpool.NewPoolFacade("mysql", connectionLimit, idleTimeout)
//
//	defer pm.Close()
//
//	var wg sync.WaitGroup
//
//	// parallel requests
//	for i := 0; i < connectionLimit+1; i++ {
//		wg.Add(1)
//		go execSleep(oneExecDurationSec, user4Db2Credentials, &wg, pm)
//	}
//
//	start := time.Now()
//	// wait
//
//	if waitTimeout(&wg, time.Second*10) {
//		assert.Fail(t, "To long query execution. Probably pool doesn't support lifetime exeed")
//	}
//
//	duration := time.Since(start)
//
//	dirationIs4Sec := math.Round(duration.Seconds())
//	assert.Equal(t, expectedTotalDurationSec, dirationIs4Sec)
//
//	// check open connections
//	stats := pm.Stats()
//	assert.Equal(t, connectionLimit, stats.NumIdle)
//}

// the pool will release idle connection if limit
func TestConnectionLifetimeExeeded(t *testing.T) {
	t.Parallel()

	// create pool
	pm := cpool.NewPoolFacade("mysql", 1, time.Millisecond)
	defer pm.Close()

	// exec
	_, err := pm.Exec(user5Db2Credentials, "select 1;")
	assert.NoError(t, err)

	// sleep for delete idle connection
	time.Sleep(time.Second)

	// check that connection killed
	stats := pm.Stats()
	assert.Equal(t, 0, stats.NumIdle)
	assert.Equal(t, 0, stats.NumOpen)
	assert.Equal(t, 1, stats.TotalMax)
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

// TODO run query on database
//execSleep делает запрос в бд. Длительность выполнения равна delaySec секунд
func execSleep(delaySec int, credentials cpool.Credentials, wg *sync.WaitGroup, pm *cpool.PoolFacade) {
	_, err := pm.Exec(credentials, "select sleep("+strconv.Itoa(delaySec)+")")
	if err != nil {
		panic(err)
	}
	wg.Done()

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
