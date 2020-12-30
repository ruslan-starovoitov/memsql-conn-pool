package memsql_conn_pool

import (
	"memsql-conn-pool/driver"
	"sync"
	"time"
)

type demo struct {
	someNumber int
}

func (d *demo) Demo() int {
	return d.someNumber
}

func (d *demo) SetDemo(value int) {
	d.someNumber = value
}

// ConnPool is a database handle representing a pool of zero or more
// underlying connections. It's safe for concurrent use by multiple
// goroutines.
//
// The sql package creates and frees connections automatically; it
// also maintains a free pool of idle connections. If the database has
// a concept of per-connection state, such state can be reliably observed
// within a transaction (Tx) or connection (Conn). Once ConnPool.Begin is called, the
// returned Tx is bound to a single connection. Once Commit or
// Rollback is called on the transaction, that transaction's
// connection is returned to ConnPool's idle connection pool. The pool size
// can be controlled with SetMaxIdleConns.
type ConnPool struct {
	demo
	poolFacade *PoolFacade

	// Atomic access only. At top of struct to prevent mis-alignment
	// on 32-bit platforms. Of type time.Duration.
	waitDuration int64 // Total time waited for new connections.

	connector driver.Connector
	// numClosed is an atomic counter which represents a total number of
	// closed connections. Stmt.openStmt checks it before cleaning closed
	// connections in Stmt.css.
	numClosed uint64

	mu       sync.Mutex // protects following fields
	freeConn []*driverConn
	//connRequests map[uint64]chan connCreationResponse
	//nextRequest  uint64 // Next key to use in connRequests.
	//TODO numInUse + numIdle
	//numOpen int // number of opened and pending open connections
	// Used to signal the need for new connections
	// a goroutine running connectionOpener() reads on this chan and
	// maybeOpenNewConnectionsLocked sends on the chan (one send per needed connection)
	// It is closed during connPool.Close(). The close tells the connectionOpener
	// goroutine to exit.
	openerCh chan struct{}

	closed       bool //TODO возможно стоит убрать
	dep          map[finalCloser]depSet
	lastPut      map[*driverConn]string // stacktrace of last conn's put; debug only
	maxIdleCount int                    // zero means defaultMaxIdleConns; negative means 0
	//TODO unlimited
	//maxOpen           int                    // <= 0 means unlimited
	maxLifetime time.Duration // maximum amount of time a connection may be reused
	maxIdleTime time.Duration // maximum amount of time a connection may be idle before being closed

	cleanerCh chan struct{} //TODO что это maxLifetime was changed or connPool was closed.
	//waitCount         int64 // Total number of connections waited for.
	maxIdleClosed     int64 // Total number of connections closed due to idle count.
	maxIdleTimeClosed int64 // Total number of connections closed due to idle time.
	maxLifetimeClosed int64 // Total number of connections closed due to max connection lifetime limit.

	stop func() // stop cancels the connection opener.
}
