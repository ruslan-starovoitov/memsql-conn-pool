package cpool

import (
	"context"
	"cpool/driver"
)

func (connPool *ConnPool) pingDC(ctx context.Context, dc *driverConn, release func(error)) error {
	var err error
	if pinger, ok := dc.ci.(driver.Pinger); ok {
		withLock(dc, func() {
			err = pinger.Ping(ctx)
		})
	}
	release(err)
	return err
}

// PingContext verifies a connection to the database is still alive,
// establishing a connection if necessary.
func (connPool *ConnPool) PingContext(ctx context.Context) error {
	var dc *driverConn
	var err error

	for i := 0; i < maxBadConnRetries; i++ {
		dc, err = connPool.conn(ctx, cachedOrNewConn)
		if err != driver.ErrBadConn {
			break
		}
	}
	if err == driver.ErrBadConn {
		dc, err = connPool.conn(ctx, alwaysNewConn)
	}
	if err != nil {
		return err
	}

	return connPool.pingDC(ctx, dc, dc.releaseConn)
}

// Ping verifies a connection to the database is still alive,
// establishing a connection if necessary.
func (connPool *ConnPool) Ping() error {
	return connPool.PingContext(context.Background())
}
