package memsql_conn_pool_tests

import (
	// "context"
	// "fmt"
	"github.com/stretchr/testify/assert"
	_ "memsql-conn-pool/mysql"
	cpool "memsql-conn-pool"
	"testing"
	"time"
)

//func Test1(t *testing.T){
//	pw, err := poolFactory.NewPool(10, time.Minute*10)
//	assert.NoError(t, err)
//	cr := Credentials{Database: "hellomemsql", Username: "root", Password: "RootPass1"}
//	statement := "select * from test"
//	rows, err := pw.Query(cr, statement)
//	assert.NoError(t, err)
//	for rows.Next(){
//		var message string
//		err = rows.Scan(&message)
//		assert.NoError(t, err)
//		t.Log(message)
//	}
//}

var rootCredentials = cpool.Credentials{
	Username: "root",
	Password: "RootPass1",
	Database: "hellomemsql",
}
var credentials = []cpool.Credentials{
	rootCredentials,
	{
		Username: "user1",
		Password: "pass1",
		Database: "hellomemsql1",
	},
	{
		Username: "user2",
		Password: "pass2",
		Database: "hellomemsql1",
	},
	{
		Username: "user3",
		Password: "pass3",
		Database: "hellomemsql1",
	},
	{
		Username: "user4",
		Password: "pass4",
		Database: "hellomemsql2",
	},
	{
		Username: "user5",
		Password: "pass5",
		Database: "hellomemsql2",
	},
	{
		Username: "user6",
		Password: "pass6",
		Database: "hellomemsql2",
	},
}

var concurrenyLevel = 10
var idleTimeout = time.Minute

func TestCrateDBConnect(t *testing.T) {
	t.Parallel()

	pm := cpool.NewPoolManager(concurrenyLevel, idleTimeout)
	defer pm.Close()
	
	var result int
	row, err := pm.QueryRow(rootCredentials, "select 1 +1")
	row.Scan(&result)
	if err != nil {
		t.Fatalf("QueryRow Scan unexpectedly failed: %v", err)
	}
	if result != 2 {
		t.Errorf("bad result: %d", result)
	}
}


//TODO split into 3 methods
func TestConnect(t *testing.T) {
	t.Parallel()
	
	//Open pool
	pm := cpool.NewPoolManager(concurrenyLevel, idleTimeout)

	//Check database name
	var currentDB string
	row, err := pm.QueryRow(rootCredentials, "SELECT DATABASE();")
	assert.NoError(t, err, "connection error")
	err = row.Scan(&currentDB)
	assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
	assert.Equal(t, rootCredentials.Database, currentDB, "Did not connect to specified database ")
	
	//Check user name 
	var user string
	row, err = pm.QueryRow(rootCredentials, "select current_user")
	assert.NoError(t, err, "connection error")
	err = row.Scan(&user)
	assert.NoError(t, err, "QueryRow Scan unexpectedly failed")
	assert.Equal(t, rootCredentials.Username+"@%",  user, "Did not connect as specified user")
	
	//Check pool closing
	pm.Close()
	row, err = pm.QueryRow(rootCredentials, "select 1 +1")
	assert.Error(t, err, "Unable to close connection")
}