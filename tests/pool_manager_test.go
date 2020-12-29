package memsql_conn_pool_tests

import (
	"math"
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

var concurrenyLevel = 10
var idleTimeout = time.Minute

func TestCrateDBConnect(t *testing.T) {
	t.Parallel()

	pm := cpool.NewPoolFacade(concurrenyLevel, idleTimeout)
	defer pm.Close()

	var result int
	row, err := pm.QueryRow(user1Db1Credentials, "select 1 +1")
	err = row.Scan(&result)
	assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
	assert.Equal(t, 2, result, "bad result")
}

func TestConnect(t *testing.T) {
	t.Parallel()

	//Open pool
	pm := cpool.NewPoolFacade(concurrenyLevel, idleTimeout)

	//Check database name
	var currentDB string
	row, err := pm.QueryRow(user1Db1Credentials, "SELECT DATABASE();")
	assert.NoError(t, err, "connection error")
	err = row.Scan(&currentDB)
	assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
	assert.Equal(t, user1Db1Credentials.Database, currentDB, "Did not connect to specified database ")

	//Check user name
	var user string
	row, err = pm.QueryRow(user1Db1Credentials, "select current_user")
	assert.NoError(t, err, "connection error")
	err = row.Scan(&user)
	assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
	assert.Equal(t, user1Db1Credentials.Username+"@%", user, "Did not connect as specified user")

	//Check pool closing
	pm.Close()
	row, err = pm.QueryRow(user1Db1Credentials, "select 1 +1")
	assert.Error(t, err, "Unable to close connection")
}

func TestExecFailure(t *testing.T) {
	t.Parallel()

	pm := cpool.NewPoolFacade(concurrenyLevel, idleTimeout)

	_, err := pm.Exec(user1Db1Credentials, "incorrect sql statement;")
	assert.Error(t, err, "connection error")

	_, err = pm.Exec(user1Db1Credentials, "select 1;")
	assert.NoError(t, err, "Exec failure appears to have broken connection")
	pm.Close()
}

func TestExecFailureCloseBefore(t *testing.T) {
	t.Parallel()

	pm := cpool.NewPoolFacade(concurrenyLevel, idleTimeout)
	pm.Close()

	_, err := pm.Exec(user1Db1Credentials, "select 1")
	require.Error(t, err)
}

//the pool will release idle connection if limit
func TestPoolManagerWillReleaseIdleConnectionIfLimitExceeded(t *testing.T) {
	t.Parallel()

	//create pool
	pm := cpool.NewPoolFacade(2, idleTimeout)
	var wg sync.WaitGroup

	//two parallel requests
	wg.Add(2)
	go ExecSleep(2, user1Db1Credentials, &wg, pm)
	go ExecSleep(2, user1Db1Credentials, &wg, pm)

	//wait
	wg.Wait()

	//check open conenctions
	stats := pm.Stats()
	assert.Equal(t, 2, stats.NumIdle)

	//request to another data source
	wg.Add(1)
	go ExecSleep(2, user1Db1Credentials, &wg, pm)

	if waitTimeout(&wg, time.Second*5) {
		assert.Fail(t, "Too long query execution. Probapbly pool doesn't support idle connections closing")
	}
}

//the pool will release idle connection if limit
func TestNumberOfIdleConnectionsOneUser(t *testing.T) {
	t.Parallel()

	//create pool
	conenctionLimit := 5
	pm := cpool.NewPoolFacade(conenctionLimit, idleTimeout)
	var wg sync.WaitGroup

	//parallel requests
	for i := 0; i < conenctionLimit; i++ {
		go ExecSleep(2, user1Db1Credentials, &wg, pm)
		wg.Add(1)
	}

	//wait
	wg.Wait()

	//check open conenctions
	stats := pm.Stats()
	assert.Equal(t, conenctionLimit, stats.NumIdle)
}

//the pool will release idle connection if limit
func TestNumberOfIdleConnectionsMultipleUsers(t *testing.T) {
	t.Parallel()

	//create pool
	conenctionLimit := 5
	pm := cpool.NewPoolFacade(conenctionLimit, idleTimeout)
	var wg sync.WaitGroup

	//parallel requests
	for i := 0; i < conenctionLimit; i++ {
		go ExecSleep(2, credentials[i], &wg, pm)
		wg.Add(1)
	}

	//wait
	wg.Wait()

	//check open conenctions
	stats := pm.Stats()
	assert.Equal(t, conenctionLimit, stats.NumIdle)
}

//the pool will release idle connection if limit
func TestConnectionsLimit(t *testing.T) {
	t.Parallel()

	//create pool
	conenctionLimit := 5
	oneExecDurationSec := 2
	expectedTotalDurationSec := 2 * oneExecDurationSec
	pm := cpool.NewPoolFacade(conenctionLimit, idleTimeout)
	var wg sync.WaitGroup

	//parallel requests
	for i := 0; i < conenctionLimit+1; i++ {
		go ExecSleep(oneExecDurationSec, user4Db2Credentials, &wg, pm)
		wg.Add(1)
	}

	start := time.Now()
	//wait
	wg.Wait()

	duration := time.Since(start)

	dirationIs4Sec := math.Round(duration.Seconds())
	assert.Equal(t, expectedTotalDurationSec, dirationIs4Sec)

	//check open conenctions
	stats := pm.Stats()
	assert.Equal(t, conenctionLimit, stats.NumIdle)
}

//the pool will release idle connection if limit
func TestConnectionLifetimeExeeded(t *testing.T) {
	t.Parallel()

	//create pool
	pm := cpool.NewPoolFacade(1, time.Millisecond)

	//exec
	_, err := pm.Exec(user5Db2Credentials, "select 1;")
	assert.NoError(t, err)

	//sleep for delete idle connection
	time.Sleep(time.Second)

	//check that connection killed
	stats := pm.Stats()
	assert.Equal(t, 0, stats.NumIdle)
	assert.Equal(t, 0, stats.NumOpen)
	assert.Equal(t, 1, stats.TotalMax)
}

func TestImpossibleCreatePoolWithInvalidConnectionLimit(t *testing.T) {
	testCases := []struct {
		name            string
		connectionLimit int
		errorExpected   bool
	}{
		{"Connection limit is zero", 0, true},
		{"Connection limit is lower that zero", -1, true},
		{"Connection limit is greater that zero", 1, false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				errorHasOccurred := recover() != nil
				if testCase.errorExpected{
					assert.True(t, errorHasOccurred, "Expected an error, but it didn't happen. Pool size less than 1 cannot be created")
				}else{
					assert.False(t, errorHasOccurred, "Unexpected error. The pool can be any size greater than zero.")
				}
				
			}()

			_ = cpool.NewPoolFacade(testCase.connectionLimit, time.Millisecond)

		})
	}
}

//ExecSleep делает запрос в бд. Длительность выполнения равна delaySec секунд
func ExecSleep(delaySec int, credentials cpool.Credentials, wg *sync.WaitGroup, pm *cpool.PoolFacade) {
	pm.Exec(credentials, "select sleep("+strconv.Itoa(delaySec)+")")
	wg.Done()
}

// waitTimeout waits for the waitgroup for the specified max timeout.
// Returns true if waiting timed out.
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}
