package memsql_conn_pool

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	cmap "github.com/orcaman/concurrent-map"
)

var errPoolManagerClosed = errors.New("pools manager closed")
//PoolManager содержит хеш-таблицу пулов соединений. Он содержит
//общие ограничения на количество соединений
type PoolManager struct {
	pools cmap.ConcurrentMap
	ctx   context.Context

	totalMax  int
	numIdle   int
	numOpen   int
	waitCount int64 // Total number of connections waited for.

	idleTimeout time.Duration
	cancel      context.CancelFunc

	closed bool

	mu sync.Mutex // protects following fields
	connRequests  map[uint64]connRequest//для запросов на создание новых соединений
	nextRequest   uint64 // Next key to use in connRequests.
	openerChannel chan *ConnPool
}

//NewPoolManager создаёт новый пул соединений
func NewPoolManager(connectionLimit int, idleTimeout time.Duration) *PoolManager {
	log.Print("NewPool creation")
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


// Exec executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
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

// Query executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
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


// QueryRow executes a query that is expected to return at most one row.
// QueryRowContext always returns a non-nil value. Errors are deferred until
// Row's Scan method is called.
// If the query selects no rows, the *Row's Scan will return ErrNoRows.
// Otherwise, the *Row's Scan scans the first selected row and discards
// the rest.
func (poolManager *PoolManager) QueryRow(credentials Credentials, sql string) (*Row, error) {
	connPool, err := poolManager.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	row := connPool.QueryRowContext(poolManager.ctx, sql)
	return row, nil
}


//TODO нужно реализовать добавить ошибку
//ClosePool закрывает все текущие соединения и блокирует открытие новых соединений
func (poolManager *PoolManager) Close() {
	poolManager.closed = true
	poolManager.cancel()
}

func (poolManager *PoolManager) getOrCreateConnPool(credentials Credentials) (*ConnPool, error) {
	if poolManager.closed{
		return nil, errPoolManagerClosed
	}
	//Create pool if not exists
	if !poolManager.pools.Has(credentials.GetId()) {
		dsn := GetDataSourceName(credentials)
		//TODO заменить название драйвера
		connPool, err := Open("mysql", dsn)
		if err != nil {
			return nil, err
		}
		//TODO некрасиво
		connPool.poolManager = poolManager
		poolManager.pools.Set(credentials.GetId(), connPool)
		log.Print("Connection pool have just created")
		return connPool, nil
	}
	//Return pool if exists
	if tmp, ok := poolManager.pools.Get(credentials.GetId()); ok {
		log.Print("Connection pool already exists")
		return tmp.(*ConnPool), nil
	}
	return nil, unableToGetPool
}

func (poolManager *PoolManager) isOpenConnectionLimitExceeded() bool {
	return poolManager.totalMax <= poolManager.numIdle+poolManager.numOpen
}