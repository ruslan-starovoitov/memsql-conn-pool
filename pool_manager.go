package memsql_conn_pool

import (
	"context"
	"memsql-conn-pool/sql"
	"time"
)

type PoolManager struct {
	pools map[Credentials]*sql.DB
	ctx   context.Context

	totalMax      int
	currentIdle   int
	currentActive int

	idleTimeout  time.Duration
	cancel       context.CancelFunc
	releasedChan chan struct{}
}

func (pw *PoolManager) Query(credentials Credentials, sql string) (*sql.Rows, error) {
	connPool, err := pw.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	rows, err := connPool.QueryContext(pw.ctx, sql)
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (pw *PoolManager) Exec(credentials Credentials, sql string) (sql.Result, error) {
	connPool, err := pw.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	result, err := connPool.ExecContext(pw.ctx, sql)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (pw *PoolManager) ClosePool() {
	pw.cancel()
}

func newConnPoolFromCredentials(credentials Credentials) (*sql.DB, error) {
	dsn := credentials.Username + ":" + credentials.Password + "@/" + credentials.Database + "?interpolateParams=true"
	db, err := sql.Open("mysql", dsn)
	return db, err
}

func (pw *PoolManager) getOrCreateConnPool(credentials Credentials) (*sql.DB, error) {
	var db *sql.DB
	var err error

	//create pool for credentials if not exists
	if _, ok := pw.pools[credentials]; !ok {
		db, err = newConnPoolFromCredentials(credentials)
		if err != nil {
			return nil, err
		}
		//TODO add mutex
		pw.pools[credentials] = db
	}

	return db, nil
}
