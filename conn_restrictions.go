package cpool

//TODO больше не нужно
//// SetMaxIdleConns sets the maximum number of connections in the idle
//// connection pool.
////
//// If MaxOpenConns is greater than 0 but less than the new MaxIdleConns,
//// then the new MaxIdleConns will be reduced to match the MaxOpenConns limit.
////
//// If n <= 0, no idle connections are retained.
////
//// The default max idle connections is currently 2. This may change in
//// a future release.
//func (connPool *ConnPool) SetMaxIdleConns(n int) {
//	connPool.mu.Lock()
//	if n > 0 {
//		connPool.maxIdleCount = n
//	} else {
//		// No idle connections.
//		connPool.maxIdleCount = -1
//	}
//	// Make sure maxIdle doesn't exceed maxOpen
//	if connPool.maxOpen > 0 && connPool.maxIdleConnsLocked() > connPool.maxOpen {
//		connPool.maxIdleCount = connPool.maxOpen
//	}
//	var closing []*driverConn
//	idleCount := len(connPool.freeConn)
//	maxIdle := connPool.maxIdleConnsLocked()
//	if idleCount > maxIdle {
//		closing = connPool.freeConn[maxIdle:]
//		connPool.freeConn = connPool.freeConn[:maxIdle]
//	}
//	connPool.maxIdleClosed += int64(len(closing))
//	connPool.mu.Unlock()
//	for _, c := range closing {
//		c.Close()
//	}
//}

//Больше не нужно
//// SetMaxOpenConns sets the maximum number of open connections to the database.
////
//// If MaxIdleConns is greater than 0 and the new MaxOpenConns is less than
//// MaxIdleConns, then MaxIdleConns will be reduced to match the new
//// MaxOpenConns limit.
////
//// If n <= 0, then there is no limit on the number of open connections.
//// The default is 0 (unlimited).
//func (connPool *ConnPool) SetMaxOpenConns(n int) {
//	connPool.mu.Lock()
//	connPool.maxOpen = n
//	if n < 0 {
//		connPool.maxOpen = 0
//	}
//	syncMaxIdle := connPool.maxOpen > 0 && connPool.maxIdleConnsLocked() > connPool.maxOpen
//	connPool.mu.Unlock()
//	if syncMaxIdle {
//		connPool.SetMaxIdleConns(n)
//	}
//}

//// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
////
//// Expired connections may be closed lazily before reuse.
////
//// If d <= 0, connections are not closed due to a connection's age.
//func (connPool *ConnPool) SetConnMaxLifetime(d time.Duration) {
//	if d < 0 {
//		d = 0
//	}
//	connPool.mu.Lock()
//	// Wake cleaner up when lifetime is shortened.
//	if d > 0 && d < connPool.maxLifetime && connPool.cleanerCh != nil {
//		select {
//		case connPool.cleanerCh <- struct{}{}:
//		default:
//		}
//	}
//	connPool.maxLifetime = d
//	connPool.startCleanerLocked()
//	connPool.mu.Unlock()
//}
//
//// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle.
////
//// Expired connections may be closed lazily before reuse.
////
//// If d <= 0, connections are not closed due to a connection's idle time.
//func (connPool *ConnPool) SetConnMaxIdleTime(d time.Duration) {
//	if d < 0 {
//		d = 0
//	}
//	connPool.mu.Lock()
//	defer connPool.mu.Unlock()
//
//	// Wake cleaner up when idle time is shortened.
//	if d > 0 && d < connPool.maxIdleTime && connPool.cleanerCh != nil {
//		select {
//		case connPool.cleanerCh <- struct{}{}:
//		default:
//		}
//	}
//	connPool.maxIdleTime = d
//	connPool.startCleanerLocked()
//}
