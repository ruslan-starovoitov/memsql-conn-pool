package cpool

import (
	"context"
	"log"
)

type ConnPoolConnectionOpener interface {
	openNewConnection(ctx context.Context, connPool *ConnPool)
}
type MyConnPoolConnectionOpener struct {
}

func NewMyConnPoolConnectionOpener() *MyConnPoolConnectionOpener {
	return &MyConnPoolConnectionOpener{}
}

//TODO метод создаёт новое соединение и вызывает p
// Open one new connection
func (o *MyConnPoolConnectionOpener) openNewConnection(ctx context.Context, connPool *ConnPool) {
	log.Println("important openNewConnection")

	// maybeOpenNewConnections has already executed connPool.numOpened++ before it sent
	// on connPool.openerCh. This function must execute connPool.numOpened-- if the
	// connection fails or is closed before returning.

	//TODO вызов внутренней функции драйвера mysql
	// внутри происходит авторизация
	// это создание нового соединения
	// занимает много времени
	conn, err := connPool.connector.Connect(ctx)

	connPool.poolFacade.mu.Lock()
	defer connPool.poolFacade.mu.Unlock()

	if connPool.closed {
		if err == nil {
			conn.Close()
		}
		//connPool.numOpened--
		connPool.poolFacade.decrementNumOpened()
		return
	}

	if err != nil {
		//connPool.numOpened--
		connPool.poolFacade.decrementNumOpened()
		//TODO почему вызывается с nil?
		connPool.putConnectionConnPoolLocked(nil, err)

		connPool.poolFacade.mu.Unlock()
		connPool.poolFacade.maybeOpenNewConnections()
		return
	}

	dc := &driverConn{
		connPool:   connPool,
		createdAt:  nowFunc(),
		returnedAt: nowFunc(),
		ci:         conn,
	}

	if connPool.putConnectionConnPoolLocked(dc, err) {
		log.Println("important openNewConnection putConnectionConnPoolLocked success")
		connPool.addDepLocked(dc, dc)
	} else {
		log.Println("important openNewConnection putConnectionConnPoolLocked failure")
		//connPool.numOpened--
		connPool.poolFacade.decrementNumOpened()
		conn.Close()
	}
}
