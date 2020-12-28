package memsql_conn_pool

import (
	"context"
	"github.com/orcaman/concurrent-map"
	"sync"
	"time"
)

//connRequest добавляется в connRequests когда ConnPool не может использовать закешированные соединения
type connRequest struct {
	//Ссылка на пул, который сделал запрос
	connPool *ConnPool
	//Канал, который пул слушает
	responce chan connCreationResponse
}

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
	//для запросов на создание новых соединений
	connRequests  map[uint64]connRequest
	nextRequest   uint64 // Next key to use in connRequests.
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
	return poolManager.totalMax <= poolManager.numIdle+poolManager.numOpen
}

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (poolManager *PoolManager) nextRequestKeyLocked() uint64 {
	next := poolManager.nextRequest
	poolManager.nextRequest++
	return next
}

// Assumes poolManager.mu is locked.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (poolManager *PoolManager) maybeOpenNewConnectionsLocked() {
	poolManager.mu.Lock()
	numRequests := len(poolManager.connRequests)
	if poolManager.totalMax > 0 {
		numCanOpen := poolManager.totalMax - poolManager.numOpen
		if numCanOpen < numRequests {
			numRequests = numCanOpen
		}
	}

	for _, value := range poolManager.connRequests {
		poolManager.numOpen++ // optimistically
		if poolManager.closed {
			return
		}
		//Дать команду создать соединение для конкретного пула
		poolManager.openerChannel <- value.connPool

		numRequests--
		if numRequests == 0 {
			break
		}
	}

	poolManager.mu.Unlock()
}
