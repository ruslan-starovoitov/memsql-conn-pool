package cpool

import "time"

// startCleanerLocked starts connectionCleaner if needed.
func (connPool *ConnPool) startCleanerLocked() {
	if (connPool.maxLifetime > 0 || connPool.maxIdleTime > 0) &&
		//connPool.numOpen > 0 &&
		connPool.cleanerCh == nil {
		connPool.cleanerCh = make(chan struct{}, 1)
		go connPool.connectionCleaner(connPool.shortestIdleTimeLocked())
	}
}

func (connPool *ConnPool) connectionCleaner(duration time.Duration) {
	const minInterval = time.Second

	if duration < minInterval {
		duration = minInterval
	}
	t := time.NewTimer(duration)

	for {
		select {
		case <-t.C:
		case <-connPool.cleanerCh: // maxLifetime was changed or connPool was closed.
		}

		connPool.mu.Lock()

		duration = connPool.shortestIdleTimeLocked()
		if connPool.closed ||
			//connPool.numOpen == 0 ||
			duration <= 0 {
			connPool.cleanerCh = nil
			connPool.mu.Unlock()
			return
		}

		closing := connPool.connectionCleanerRunLocked()
		connPool.mu.Unlock()
		for _, c := range closing {
			c.Close()
		}

		if duration < minInterval {
			duration = minInterval
		}
		t.Reset(duration)
	}
}

func (connPool *ConnPool) connectionCleanerRunLocked() (closing []*driverConn) {
	if connPool.maxLifetime > 0 {
		expiredSince := nowFunc().Add(-connPool.maxLifetime)
		for i := 0; i < len(connPool.freeConn); i++ {
			c := connPool.freeConn[i]
			if c.createdAt.Before(expiredSince) {
				closing = append(closing, c)
				last := len(connPool.freeConn) - 1
				connPool.freeConn[i] = connPool.freeConn[last]
				connPool.freeConn[last] = nil
				connPool.freeConn = connPool.freeConn[:last]
				i--
			}
		}
		connPool.maxLifetimeClosed += int64(len(closing))
	}

	if connPool.maxIdleTime > 0 {
		expiredSince := nowFunc().Add(-connPool.maxIdleTime)
		var expiredCount int64
		for i := 0; i < len(connPool.freeConn); i++ {
			c := connPool.freeConn[i]
			if connPool.maxIdleTime > 0 && c.returnedAt.Before(expiredSince) {
				closing = append(closing, c)
				expiredCount++
				last := len(connPool.freeConn) - 1
				connPool.freeConn[i] = connPool.freeConn[last]
				connPool.freeConn[last] = nil
				connPool.freeConn = connPool.freeConn[:last]
				i--
			}
		}
		connPool.maxIdleTimeClosed += expiredCount
	}
	return
}
