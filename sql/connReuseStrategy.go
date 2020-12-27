package sql

// connReuseStrategy determines how (*ConnPool).conn returns database connections.
type connReuseStrategy uint8

const (
	// alwaysNewConn forces a new connection to the database.
	alwaysNewConn connReuseStrategy = iota
	// cachedOrNewConn returns a cached connection, if available, else waits
	// for one to become available (if MaxOpenConns has been reached) or
	// creates a new database connection.
	cachedOrNewConn
	// newOrCached creates a new connection, if avaliable,
	// else	return a cached connection
	// else waits for one to become avaliable (if MaxOpenConns has been reached)
	newOrCached
)
