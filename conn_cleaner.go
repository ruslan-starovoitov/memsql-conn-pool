package cpool

import "time"

// startCleanerLocked starts connectionCleaner if needed.
func (connPool *ConnPool) startCleanerLocked() {
	if (0 < connPool.maxLifetime || 0 < connPool.maxIdleTime) &&
		//connPool.numOpened > 0 &&
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
	timer := time.NewTimer(duration)

	for {
		select {
		case <-timer.C:
		case <-connPool.cleanerCh: // maxLifetime was changed or connPool was closed.
		}

		connPool.poolFacade.mu.Lock()

		duration = connPool.shortestIdleTimeLocked()
		if connPool.closed ||
			//connPool.numOpened == 0 ||
			duration <= 0 {
			connPool.cleanerCh = nil
			connPool.poolFacade.mu.Unlock()
			return
		}

		closing := connPool.connectionCleanerRunLocked()
		connPool.poolFacade.mu.Unlock()
		for _, c := range closing {
			c.Close()
		}

		if duration < minInterval {
			duration = minInterval
		}
		timer.Reset(duration)
	}
}

func (connPool *ConnPool) connectionCleanerRunLocked() (closing []*driverConn) {
	if connPool.maxLifetime > 0 {
		expiredSince := nowFunc().Add(-connPool.maxLifetime)
		for conn := range connPool.freeConn {
			if conn.createdAt.Before(expiredSince) {
				closing = append(closing, conn)
				delete(connPool.freeConn, conn)
			}
		}
		connPool.maxLifetimeClosed += int64(len(closing))
	}

	if connPool.maxIdleTime > 0 {
		expiredSince := nowFunc().Add(-connPool.maxIdleTime)
		var expiredCount int64

		for conn := range connPool.freeConn {
			if connPool.maxIdleTime > 0 && conn.returnedAt.Before(expiredSince) {
				closing = append(closing, conn)
				expiredCount++
				delete(connPool.freeConn, conn)
			}
		}

		connPool.maxIdleTimeClosed += expiredCount
	}
	return
}
