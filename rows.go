package memsql_conn_pool

import "memsql-conn-pool/sql"

type RowsWrapper struct {
	sql.Rows
	releasedChan chan<- struct{}
}

func (r*RowsWrapper) Close(){
	r.Close()
	r.releasedChan<- struct {}{}
}

