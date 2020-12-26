package memsql_conn_pool

import (
	"context"
	"memsql-conn-pool/sql"
	"time"
)

func NewPool(connectionLimit int, idleTimeout time.Duration) *PoolManager {
	pw := PoolManager{
		pools:        make(map[Credentials]*sql.DB),
		totalMax:     connectionLimit,
		idleTimeout:  idleTimeout,
		releasedChan: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	pw.ctx = ctx
	pw.cancel = cancel
	return &pw
}
