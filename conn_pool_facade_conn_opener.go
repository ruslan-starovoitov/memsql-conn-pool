package cpool

import (
	"context"
	"log"
)

type ConnPoolFacadeConnOpener interface {
	startCreatingConnections(ctx context.Context, connPoolFacade *ConnPoolFacade)
}

type MyConnPoolFacadeConnOpener struct {
	connOpener ConnPoolConnectionOpener
}

func NewMyConnPoolFacadeConnOpener(connOpener ConnPoolConnectionOpener) *MyConnPoolFacadeConnOpener {
	return &MyConnPoolFacadeConnOpener{connOpener: connOpener}
}

//TODO слушает команды на создание новых соединений
//Runs in a separate goroutine, opens new connections when requested.
func (m *MyConnPoolFacadeConnOpener) startCreatingConnections(ctx context.Context, connPoolFacade *ConnPoolFacade) {
	//ждём пока контекст закроется или попросят открыть новое соединение
	for {
		select {
		case <-ctx.Done():
			log.Println("ConnPoolFacade connectionOpener ctx Done")
			return
		//received a command to open a connection
		case connPool := <-connPoolFacade.openerChannel:
			stats := connPoolFacade.Stats()
			log.Printf("ConnPoolFacade connectionOpener openerChannel stats = %v\n", stats)
			m.connOpener.openNewConnection(ctx, connPool)
		}
	}
}
