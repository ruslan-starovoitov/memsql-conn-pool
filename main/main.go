package main

import (
	"log"
	cpool "memsql-conn-pool"
	_ "memsql-conn-pool/mysql"
	"sync"
	"time"
)

func main() {
	log.Print("start")
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

	connPool := cpool.NewPoolManager(100, time.Minute)
	
	reader := reader{}
	wg := sync.WaitGroup{}

	// seeder := seeder{}
	// log.Print("start writing")
	// for _, cr := range credentials {
	// 	wg.Add(1)
	// 	seeder.Seed(cr, connPool,  &wg)
	// }

	// wg.Wait()
	
	log.Print("start reading")
	for _, cr := range credentials {
		wg.Add(1)
		go reader.Read(cr, connPool, &wg)
	}

	log.Print("wait")
	wg.Wait()
	log.Print("the end")
}
