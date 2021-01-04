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
//ConnPoolFacade содержит хеш-таблицу пулов соединений. Он содержит
//общие ограничения на количество соединений
type ConnPoolFacade struct {
	driverName string //mysql

	pools cmap.ConcurrentMap //содержит ссылки на ConnPool
	ctx   context.Context

	totalMax int // <= 0 means unlimited

	numOpened int // active + idle
	waitCount int // Total number of connections waited for.

	idleTimeout time.Duration
	cancel      context.CancelFunc

	closed bool

	mu sync.Mutex // protects following fields
	//connRequests map[uint64]connRequest //для запросов на создание новых соединений
	//nextRequest uint64 // Next key to use in connRequests.

	openerChannel chan *ConnPool

	lruCache *lru.Cache
}

//NewPoolFacade создаёт новый пул соединений
//если connectionLimit меньше или равен нулю, то ограничения нет
// если idleTimeout будет установлен меньше, чем одна секунда, то он автоматически будет заменен на одну секунду
func NewPoolFacade(driverName string, connectionLimit int, idleTimeout time.Duration) *ConnPoolFacade {
	//TODO fix
	//if _, ok := drivers[driverName]; !ok {
	//	panic("Unexpected driver name. Please register driver.")
	//}

	//TODO Lru упадет, если передать аргумент меньше единицы
	lruCache, err := lru.New(connectionLimit)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	poolFacade := ConnPoolFacade{
		driverName:  driverName,
		pools:       cmap.New(),
		ctx:         ctx,
		totalMax:    connectionLimit,
		idleTimeout: idleTimeout,
		cancel:      cancel,
		closed:      false,
		mu:          sync.Mutex{},
		lruCache:    lruCache,
		//TODO magic number
		openerChannel: make(chan *ConnPool, 1000),
	}

	//TODO запуск создания новых соединений
	go poolFacade.connectionOpener(ctx)
	return &poolFacade
}

// Exec executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
func (connPoolFacade *ConnPoolFacade) Exec(credentials Credentials, sql string) (Result, error) {
	connPool, err := connPoolFacade.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	result, err := connPool.ExecContext(connPoolFacade.ctx, sql)
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
func (connPoolFacade *ConnPoolFacade) BeginTx(credentials Credentials) (*Tx, error) {
	connPool, err := connPoolFacade.getOrCreateConnPool(credentials)
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
func (connPoolFacade *ConnPoolFacade) Query(credentials Credentials, sql string) (*Rows, error) {
	connPool, err := connPoolFacade.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	rows, err := connPool.QueryContext(connPoolFacade.ctx, sql)
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
func (connPoolFacade *ConnPoolFacade) QueryRow(credentials Credentials, sql string) (*Row, error) {
	connPool, err := connPoolFacade.getOrCreateConnPool(credentials)
	if err != nil {
		return nil, err
	}

	row := connPool.QueryRowContext(connPoolFacade.ctx, sql)
	return row, nil
}

//TODO нужно реализовать добавить ошибку
//ClosePool закрывает все текущие соединения и блокирует открытие новых соединений
func (connPoolFacade *ConnPoolFacade) Close() {
	connPoolFacade.closed = true
	connPoolFacade.cancel()
}

var errPoolFacadeClosed = errors.New("pools facade closed")

//getOrCreateConnPool создаёт новый пустой пул для нового data source name если его нет
func (connPoolFacade *ConnPoolFacade) getOrCreateConnPool(credentials Credentials) (*ConnPool, error) {
	if connPoolFacade.closed {
		return nil, errPoolFacadeClosed
	}
	//Create pool if not exists
	if !connPoolFacade.pools.Has(credentials.GetId()) {
		dsn := GetDataSourceName(credentials)
		//TODO заменить название драйвера
		//TODO это плохо что общая библиотека зависит от mysql
		//нужно, чтобы название драйвера устанавливалось при инициализации пула
		connPool, err := Open(connPoolFacade.driverName, dsn)
		if err != nil {
			return nil, err
		}
		//TODO некрасиво
		connPool.poolFacade = connPoolFacade
		connPool.SetConnMaxIdleTime(connPoolFacade.idleTimeout)
		connPoolFacade.pools.Set(credentials.GetId(), connPool)
		return connPool, nil
	}
	//Return pool if exists
	if tmp, ok := connPoolFacade.pools.Get(credentials.GetId()); ok {
		return tmp.(*ConnPool), nil
	}
	return nil, unableToGetPool
}

func (connPoolFacade *ConnPoolFacade) isOpenConnectionLimitExceeded() bool {
	connPoolFacade.mu.Lock()
	result := connPoolFacade.totalMax <= connPoolFacade.numOpened
	log.Printf("ConnPoolFacade isOpenConnectionLimitExceeded %v  %v  %v\n", result, connPoolFacade.totalMax, connPoolFacade.numOpened)
	connPoolFacade.mu.Unlock()
	return result
}

func (connPoolFacade *ConnPoolFacade) isConnectionLimitExistsLocked() bool {
	return 0 < connPoolFacade.totalMax
}

func (connPoolFacade *ConnPoolFacade) getNumOfConnRequestsLocked() int {
	return connPoolFacade.waitCount
}

func (connPoolFacade *ConnPoolFacade) getConnPoolsThatHaveRequestedNewConnections(connectionsNum int) []*ConnPool {
	if connectionsNum == 0 {
		return nil
	}

	log.Printf("connectionsNum = %v\n", connectionsNum)

	log.Printf("facade wait count = %v\n", connPoolFacade.waitCount)
	var slice []*ConnPool
	connectionRemaining := connectionsNum

	log.Printf("len of pools %v", connPoolFacade.pools.Count())
	//пройтись по пулам
	for tuple := range connPoolFacade.pools.IterBuffered() {
		connPool, ok := tuple.Val.(*ConnPool)
		log.Printf("conn pool wait count = %v\n", connPool.waitCount)

		if !ok {
			panic("В словаре пулов лежит структура неизвестного типа.")
		}

		if connPool.waitCount == 0 {
			continue
		}

		numOfNewConn := min(connPool.waitCount, connectionRemaining)
		for i := 0; i < numOfNewConn; i++ {
			slice = append(slice, connPool)
		}

		connectionRemaining -= numOfNewConn
		log.Printf("connectionRemaining = %v, numOfNewConn = %v\n", connectionRemaining, numOfNewConn)
		if connectionRemaining == 0 {
			return slice
		}
	}

	panic("Неправильно вычислено кол-во простаивающих соединений")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

//func (connPoolFacade *ConnPoolFacade) isFreeConnectionsNeeded() bool {
//	connPoolFacade.mu.Lock()
//	result := 0 < len(connPoolFacade.connRequests)
//	connPoolFacade.mu.Unlock()
//	return result
//}
