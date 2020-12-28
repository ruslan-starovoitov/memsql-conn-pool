package memsql_conn_pool

import (
	"context"
	"errors"
	"github.com/orcaman/concurrent-map"
	"time"
)

var unableToGetPool = errors.New("unable get pool from map")

//PoolManager содержит хеш-таблицу пулов соединений. Он содержит
//общие ограничения на количество соединений
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

func (pm *PoolManager) Query(credentials Credentials, sql string) (*Rows, error) {
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

func (pm *PoolManager) Exec(credentials Credentials, sql string) (Result, error) {
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

func (pm *PoolManager) getOrCreateConnPool(credentials Credentials) (*ConnPool, error) {
	//Create pool if not exists
	if !pm.pools.Has(credentials.GetId()) {
		dsn := GetDataSourceName(credentials)
		//TODO заменить название драйвера
		connPool, err := Open("mysql", dsn)
		if err != nil {
			return nil, err
		}
		pm.pools.Set(credentials.GetId(), connPool)
		return connPool, nil
	}
	//Return pool if exists
	if tmp, ok := pm.pools.Get(credentials.GetId()); ok {
		return tmp.(*ConnPool), nil
	}
	return nil, unableToGetPool
}
