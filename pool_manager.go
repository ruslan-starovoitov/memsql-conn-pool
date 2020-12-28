package memsql_conn_pool

import (
	"context"
	"github.com/orcaman/concurrent-map"
	"time"
)

//PoolManager содержит хеш-таблицу пулов соединений. Он содержит
//общие ограничения на количество соединений
type PoolManager struct {
	pools cmap.ConcurrentMap
	ctx   context.Context

	totalMax  int
	numIdle   int
	numActive int

	idleTimeout time.Duration
	cancel      context.CancelFunc

	openerChannel chan *ConnPool
}

func NewPool(connectionLimit int, idleTimeout time.Duration) *PoolManager {
	poolManager := PoolManager{
		pools:       cmap.New(),
		totalMax:    connectionLimit,
		idleTimeout: idleTimeout,
	}
	ctx, cancel := context.WithCancel(context.Background())
	poolManager.ctx = ctx
	poolManager.cancel = cancel

	//TODO запуск создания новых соединений
	go poolManager.globalConnectionOpener(ctx)
	return &poolManager
}

//TODO создаёт новое соединение в контексте без проверок ограничений
// копия connectionOpener
func (poolManager *PoolManager) globalConnectionOpener(ctx context.Context) {
	//ждём пока контекст закроется или попросят открыть новое соединение
	for {
		select {
		case <-ctx.Done():
			return
		case connPool := <-poolManager.openerChannel:
			connPool.openNewConnection(ctx)
		}
	}
}

func (poolManager *PoolManager) Exec(credentials Credentials, sql string) (Result, error) {
	connPool, err := poolManager.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	result, err := connPool.ExecContext(poolManager.ctx, sql)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (poolManager *PoolManager) Query(credentials Credentials, sql string) (*Rows, error) {
	connPool, err := poolManager.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	rows, err := connPool.QueryContext(poolManager.ctx, sql)
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (poolManager *PoolManager) ClosePool() {
	poolManager.cancel()
}

func (poolManager *PoolManager) getOrCreateConnPool(credentials Credentials) (*ConnPool, error) {
	//Create pool if not exists
	if !poolManager.pools.Has(credentials.GetId()) {
		dsn := GetDataSourceName(credentials)
		//TODO заменить название драйвера
		connPool, err := Open("mysql", dsn)
		if err != nil {
			return nil, err
		}
		poolManager.pools.Set(credentials.GetId(), connPool)
		return connPool, nil
	}
	//Return pool if exists
	if tmp, ok := poolManager.pools.Get(credentials.GetId()); ok {
		return tmp.(*ConnPool), nil
	}
	return nil, unableToGetPool
}

func (poolManager *PoolManager) isOpenConnectionLimitExceeded() bool {
	return poolManager.totalMax <= poolManager.numIdle+poolManager.numActive
}
