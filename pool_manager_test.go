package memsql_conn_pool

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	_ "memsql-conn-pool/mysql"
	"memsql-conn-pool/sql"
	"testing"
	"time"
)

//var poolFactory = PoolFactory{}

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

func Helper(doneChan <-chan struct{}) {
	select {
	case <-doneChan:
		fmt.Println("Done in helper method")
	}
}
func TestConnectionClosing(t *testing.T) {
	cr := Credentials{Database: "hellomemsql", Username: "root", Password: "RootPass1"}
	connString := cr.Username + ":" + cr.Password + "@/" + cr.Database
	db, err := sql.Open("mysql", connString)
	assert.NoError(t, err)
	connContext := context.Background()
	go Helper(connContext.Done())
	connetion, err := db.Conn(connContext)
	assert.NoError(t, err)
	err = connetion.PingContext(context.Background())
	assert.NoError(t, err)
	err = connetion.Close()

	assert.NoError(t, err)
	select {
	case <-connContext.Done():
		fmt.Println("success")
	case <-time.After(time.Second):
		t.Error("timeout error")
	}
}
