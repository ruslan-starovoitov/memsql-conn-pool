package main

import (
	cpool "memsql-conn-pool"
	_ "memsql-conn-pool/mysql"
	"sync"
	"time"
)

var credentials = []cpool.Credentials{
	{
		Username: "root",
		Password: "RootPass1",
		Database: "hellomemsql",
	},
	{
		Username: "user1",
		Password: "pass1",
		Database: "hellomemsql1",
	},
	{
		Username: "user2",
		Password: "pass2",
		Database: "hellomemsql1",
	},
	{
		Username: "user3",
		Password: "pass3",
		Database: "hellomemsql1",
	},
	{
		Username: "user4",
		Password: "pass4",
		Database: "hellomemsql2",
	},
	{
		Username: "user5",
		Password: "pass5",
		Database: "hellomemsql2",
	},
	{
		Username: "user6",
		Password: "pass6",
		Database: "hellomemsql2",
	},
}

func main() {
	println("start")
	connPool := cpool.NewPool(100, time.Minute)
	seeder := seeder{}
	reader := reader{}
	wg := sync.WaitGroup{}

	for _, cr := range credentials {
		seeder.Seed(cr, connPool)
	}

	for _, cr := range credentials {
		wg.Add(1)
		go reader.Read(cr, connPool, &wg)
	}

	wg.Wait()
	println("the end")
}
