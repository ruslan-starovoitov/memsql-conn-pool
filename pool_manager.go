package memsql_conn_pool

import (
	"context"
	"errors"
	"github.com/orcaman/concurrent-map"
	"memsql-conn-pool/sql"
	"time"
)

type PoolManager struct {
	pools cmap.ConcurrentMap
	ctx   context.Context

	totalMax      int
	currentIdle   int
	currentActive int

	idleTimeout  time.Duration
	cancel       context.CancelFunc
	releasedChan chan struct{}
}

func (pm *PoolManager) Query(credentials Credentials, sql string) (*sql.Rows, error) {
	connPool, err := pm.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	rows, err := connPool.QueryContext(pm.ctx, sql)
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (pm *PoolManager) Exec(credentials Credentials, sql string) (sql.Result, error) {
	connPool, err := pm.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	result, err := connPool.ExecContext(pm.ctx, sql)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (pm *PoolManager) ClosePool() {
	pm.cancel()
}

func newConnPoolFromCredentials(credentials Credentials) (*sql.ConnPool, error) {
	dsn := GetDataSourceName(credentials)
	db, err := sql.Open("mysql", dsn)
	return db, err
}

var unableToGetPool = errors.New("unable get pool from map")

func (pm *PoolManager) getOrCreateConnPool(credentials Credentials) (*sql.ConnPool, error) {
	//Create pool if not exists
	if !pm.pools.Has(credentials.GetId()) {
		connPool, err := newConnPoolFromCredentials(credentials)
		if err != nil {
			return nil, err
		}
		pm.pools.Set(credentials.GetId(), connPool)
		return connPool, nil
	}
	//Return pool if exists
	if tmp, ok := pm.pools.Get(credentials.GetId()); ok {
		return tmp.(*sql.ConnPool), nil
	}
	return nil, unableToGetPool
}
