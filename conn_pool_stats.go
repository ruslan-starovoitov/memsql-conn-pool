package cpool

import (
	"sync/atomic"
	"time"
)

// ConnPoolStats contains database statistics.
type ConnPoolStats struct {
	//TODO unlimited
	//MaxOpenConnections int // Maximum number of open connections to the database.

	// Pool Status
	OpenConnections int // The number of established connections both in use and idle.
	InUse           int // The number of connections currently in use.
	Idle            int // The number of idle connections.

	// Counters
	//WaitCount         int64         // The total number of connections waited for.
	WaitCount         int           // The total number of connections waited for.
	WaitDuration      time.Duration // The total time blocked waiting for a new connection.
	MaxIdleClosed     int64         // The total number of connections closed due to SetMaxIdleConns.
	MaxIdleTimeClosed int64         // The total number of connections closed due to SetConnMaxIdleTime.
	MaxLifetimeClosed int64         // The total number of connections closed due to SetConnMaxLifetime.
}

// Stats returns database statistics.
func (connPool *ConnPool) Stats() ConnPoolStats {
	wait := atomic.LoadInt64(&connPool.waitDuration)

	connPool.poolFacade.mu.Lock()
	defer connPool.poolFacade.mu.Unlock()

	stats := ConnPoolStats{
		//MaxOpenConnections: connPool.maxOpen,

		Idle: len(connPool.freeConn),
		//OpenConnections: connPool.numOpened,
		//TODO добавить подсчёт InUse
		//InUse:           connPool.numOpened - len(connPool.freeConn),

		//WaitCount:         connPool.waitCount,
		WaitDuration:      time.Duration(wait),
		MaxIdleClosed:     connPool.maxIdleClosed,
		MaxIdleTimeClosed: connPool.maxIdleTimeClosed,
		MaxLifetimeClosed: connPool.maxLifetimeClosed,
	}
	return stats
}
