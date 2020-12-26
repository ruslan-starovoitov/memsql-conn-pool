package memsql_conn_pool

import (
	"context"
	"database/sql"
	"time"
)

type PoolWrapper struct{
	pools map[Credentials]*sql.DB
	ctx context.Context

	totalMax int
	currentIdle int
	currentActive int

	idleTimeout time.Duration
	cancel context.CancelFunc //TODO delete cancel. its dangerous
	releasedChan chan struct{}
}

func (pw* PoolWrapper) Query(credentials Credentials, sql string) (*sql.Rows, error){
	connection, err := pw.getDatabaseConnection(credentials)
	if err != nil{
		return nil, err
	}

	rows, err := connection.QueryContext(pw.ctx, sql)
	if err != nil{
		return nil, err
	}

	return rows, nil
}

func (pw* PoolWrapper) Exec(credentials Credentials, sql string) (sql.Result, error) {
	connection, err := pw.getDatabaseConnection(credentials)
	if err != nil{
		return nil, err
	}

	result, err := connection.ExecContext(pw.ctx, sql)
	if err != nil{
		return nil, err
	}

	return result, nil
}

func (pw* PoolWrapper) ClosePool(){
	pw.cancel()
}

func (pw* PoolWrapper) canCreateNewConnection() bool{
	return pw.currentIdle<pw.totalMax
}

func (pw* PoolWrapper) getDatabaseConnection(credentials Credentials) (*sql.Conn, error){
	if db, ok := pw.pools[credentials]; ok{
		//pool for this credentials exists
		//TODO How to ensure that connection created
		newContext:=context.Background()
		connection, err := db.Conn(newContext)
		connection.Close()

		//try to get idle connection or create new

		return connection, err
	}else {
		//Try to create new connection
	}
	panic(notImplemented)
}

