package memsql_conn_pool

import (
	"context"
	"time"
)

func NewPool(connectionLimit int, idleTimeout time.Duration) IPool {
	pw := PoolWrapper{}
	ctx, cancel := context.WithCancel(context.Background())
	pw.ctx = ctx
	pw.cancel = cancel
	return &pw
}
