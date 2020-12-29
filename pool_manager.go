package memsql_conn_pool

import (
	"context"
	"errors"
	"sync"
	"time"

	cmap "github.com/orcaman/concurrent-map"
)

//PoolFacade содержит хеш-таблицу пулов соединений. Он содержит
//общие ограничения на количество соединений
type PoolFacade struct {
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

//NewPoolFacade создаёт новый пул соединений
func NewPoolFacade(connectionLimit int, idleTimeout time.Duration) *PoolFacade {
	if connectionLimit <= 0{
		panic("it doesn't make sense to create a pool with such connection limit ")
	}
	poolManager := PoolFacade{
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
func (poolManager *PoolFacade) Exec(credentials Credentials, sql string) (Result, error) {
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

// BeginTx starts a transaction.
//
// The provided context is used until the transaction is committed or rolled back.
// If the context is canceled, the sql package will roll back
// the transaction. Tx.Commit will return an error if the context provided to
// BeginTx is canceled.
//
// The provided TxOptions is optional and may be nil if defaults should be used.
// If a non-default isolation level is used that the driver doesn't support,
// an error will be returned.
func (poolManager *PoolFacade) BeginTx(credentials Credentials) (*Tx, error) {
	connPool, err := poolManager.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	tx, err := connPool.BeginTx(context.Background(),  nil)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

// Query executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
func (poolManager *PoolFacade) Query(credentials Credentials, sql string) (*Rows, error) {
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
func (poolManager *PoolFacade) QueryRow(credentials Credentials, sql string) (*Row, error) {
	connPool, err := poolManager.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	row := connPool.QueryRowContext(poolManager.ctx, sql)
	return row, nil
}


//TODO нужно реализовать добавить ошибку
//ClosePool закрывает все текущие соединения и блокирует открытие новых соединений
func (poolManager *PoolFacade) Close() {
	poolManager.closed = true
	poolManager.cancel()
}


var errPoolManagerClosed = errors.New("pools manager closed")
//getOrCreateConnPool создаёт новый пустой пул для нового data source name если его нет
func (poolManager *PoolFacade) getOrCreateConnPool(credentials Credentials) (*ConnPool, error) {
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
		return connPool, nil
	}
	//Return pool if exists
	if tmp, ok := poolManager.pools.Get(credentials.GetId()); ok {
		return tmp.(*ConnPool), nil
	}
	return nil, unableToGetPool
}

//TODO возможно нужен мьютекс
func (poolManager *PoolFacade) isOpenConnectionLimitExceeded() bool {
	return poolManager.totalMax <= poolManager.numOpen
}