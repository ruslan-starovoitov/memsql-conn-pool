package main

import (
	cpool "cpool"
	"sync"
)

type seeder struct {
}

func (seeder) Seed(credentials cpool.Credentials, manager *cpool.ConnPoolFacade, group *sync.WaitGroup) {
	for i := 0; i < 10; i++ {
		_, err := manager.Exec(credentials, "insert into test (message)values(\"my message\")")
		if err != nil {
			println("13 " + err.Error())
		}
	}
	group.Done()
}
