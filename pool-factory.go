package memsql_conn_pool

import (
	"context"
	cmap "github.com/orcaman/concurrent-map"
	"time"
)

func NewPool(connectionLimit int, idleTimeout time.Duration) *PoolManager {
	pw := PoolManager{
		pools:        cmap.New(),
		totalMax:     connectionLimit,
		idleTimeout:  idleTimeout,
		releasedChan: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	pw.ctx = ctx
	pw.cancel = cancel
	return &pw
}
