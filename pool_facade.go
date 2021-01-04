package cpool

import (
	"context"
	"errors"
	lru "github.com/hashicorp/golang-lru"
	"log"
	"sync"
	"time"

	cmap "github.com/orcaman/concurrent-map"
)

// driverConn wraps a driver.Conn with a mutex, to
// be held during all calls into the Conn. (including any calls onto
// interfaces returned via that Conn, such as calls on Tx, Stmt,
//PoolFacade содержит хеш-таблицу пулов соединений. Он содержит
//общие ограничения на количество соединений
type PoolFacade struct {
	driverName string //mysql

	pools cmap.ConcurrentMap //содержит ссылки на ConnPool
	ctx   context.Context

	totalMax int // <= 0 means unlimited
	//numIdle   int
	numOpen   int
	waitCount int64 // Total number of connections waited for.

	idleTimeout time.Duration
	cancel      context.CancelFunc

	closed bool

	mu           sync.Mutex             // protects following fields
	connRequests map[uint64]connRequest //для запросов на создание новых соединений
	nextRequest  uint64                 // Next key to use in connRequests.

	openerChannel chan *ConnPool

	lruCache *lru.Cache
}

//NewPoolFacade создаёт новый пул соединений
//если connectionLimit меньше или равен нулю, то ограничения нет
// если idleTimeout будет установлен меньше, чем одна секунда, то он автоматически будет заменен на одну секунду
func NewPoolFacade(driverName string, connectionLimit int, idleTimeout time.Duration) *PoolFacade {
	if _, ok := drivers[driverName]; !ok {
		panic("Unexpected driver name. Please register driver.")
	}

	//TODO Lru упадет, если передать аргумент меньше единицы
	lruCache, err := lru.New(connectionLimit)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	poolFacade := PoolFacade{
		driverName:   driverName,
		pools:        cmap.New(),
		ctx:          ctx,
		totalMax:     connectionLimit,
		idleTimeout:  idleTimeout,
		cancel:       cancel,
		closed:       false,
		mu:           sync.Mutex{},
		connRequests: make(map[uint64]connRequest),
		nextRequest:  0,
		lruCache:     lruCache,
		//TODO magic number
		openerChannel: make(chan *ConnPool, 1000),
	}

	//TODO запуск создания новых соединений
	go poolFacade.connectionOpener(ctx)
	return &poolFacade
}

// Exec executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
func (poolFacade *PoolFacade) Exec(credentials Credentials, sql string) (Result, error) {
	connPool, err := poolFacade.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	result, err := connPool.ExecContext(poolFacade.ctx, sql)
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
func (poolFacade *PoolFacade) BeginTx(credentials Credentials) (*Tx, error) {
	connPool, err := poolFacade.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	tx, err := connPool.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

// Query executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
func (poolFacade *PoolFacade) Query(credentials Credentials, sql string) (*Rows, error) {
	connPool, err := poolFacade.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	rows, err := connPool.QueryContext(poolFacade.ctx, sql)
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
func (poolFacade *PoolFacade) QueryRow(credentials Credentials, sql string) (*Row, error) {
	connPool, err := poolFacade.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	row := connPool.QueryRowContext(poolFacade.ctx, sql)
	return row, nil
}

//TODO нужно реализовать добавить ошибку
//ClosePool закрывает все текущие соединения и блокирует открытие новых соединений
func (poolFacade *PoolFacade) Close() {
	poolFacade.closed = true
	poolFacade.cancel()
}

var errPoolFacadeClosed = errors.New("pools manager closed")

//getOrCreateConnPool создаёт новый пустой пул для нового data source name если его нет
func (poolFacade *PoolFacade) getOrCreateConnPool(credentials Credentials) (*ConnPool, error) {
	if poolFacade.closed {
		return nil, errPoolFacadeClosed
	}
	//Create pool if not exists
	if !poolFacade.pools.Has(credentials.GetId()) {
		dsn := GetDataSourceName(credentials)
		//TODO заменить название драйвера
		//TODO это плохо что общая библиотека зависит от mysql
		//нужно, чтобы название драйвера устанавливалось при инициализации пула
		connPool, err := Open(poolFacade.driverName, dsn)
		if err != nil {
			return nil, err
		}
		//TODO некрасиво
		connPool.poolFacade = poolFacade
		connPool.SetConnMaxIdleTime(poolFacade.idleTimeout)
		poolFacade.pools.Set(credentials.GetId(), connPool)
		return connPool, nil
	}
	//Return pool if exists
	if tmp, ok := poolFacade.pools.Get(credentials.GetId()); ok {
		return tmp.(*ConnPool), nil
	}
	return nil, unableToGetPool
}

func (poolFacade *PoolFacade) isOpenConnectionLimitExceeded() bool {
	poolFacade.mu.Lock()
	result := poolFacade.totalMax <= poolFacade.numOpen
	log.Printf("PoolFacade isOpenConnectionLimitExceeded %v  %v  %v\n", result, poolFacade.totalMax, poolFacade.numOpen)
	poolFacade.mu.Unlock()
	return result
}

func (poolFacade *PoolFacade) isConnectionLimitExistsLocked() bool {
	return 0 < poolFacade.totalMax
}

func (poolFacade *PoolFacade) isFreeConnectionsNeeded() bool {
	poolFacade.mu.Lock()
	result := 0 < len(poolFacade.connRequests)
	poolFacade.mu.Unlock()
	return result
}
