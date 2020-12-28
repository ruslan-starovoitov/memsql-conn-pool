package main

import (
	cpool "memsql-conn-pool"
)

type seeder struct {
}

func (seeder) Seed(credentials cpool.Credentials, manager *cpool.PoolManager) {
	for i := 0; i < 100; i++ {
		_, err := manager.Exec(credentials, "insert into test (message)values(\"my message\")")
		if err != nil {
			println("13 " + err.Error())
		}
	}
}
