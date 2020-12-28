package main

import (
	pool "memsql-conn-pool"
	"sync"
	"time"
)

var credentials = []pool.Credentials{
	//{
	//	Username: "root",
	//	Password: "RootPass1",
	//	Database: "hellomemsql",
	//},
	//{
	//	Username: "user1",
	//	Password: "pass1",
	//	Database: "hellomemsql1",
	//},
	//{
	//	Username: "user2",
	//	Password: "pass2",
	//	Database: "hellomemsql2",
	//},
	//{
	//	Username: "user3",
	//	Password: "pass3",
	//	Database: "hellomemsql3",
	//},
	{
		Username: "user9",
		Password: "pass9",
		Database: "hellomemsql7",
	},
}

func main() {
	println("start")
	connPool := pool.NewPool(100, time.Minute)
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
