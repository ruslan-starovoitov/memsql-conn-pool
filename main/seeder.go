package main

import pool "memsql-conn-pool"

type seeder struct {
}

func (seeder) Seed(credentials pool.Credentials, manager *pool.PoolManager) {
	for i := 0; i < 100; i++ {
		_, err := manager.Exec(credentials, "insert into test (message)values(\"my message\")")
		if err != nil {
			println("13 " + err.Error())
		}
	}
}
