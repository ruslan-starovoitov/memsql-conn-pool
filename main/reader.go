package main

import (
	cpool "cpool"
	"log"
	"sync"
)

type reader struct {
}

func (reader) Read(credentials cpool.Credentials, connPool *cpool.ConnPoolFacade, group *sync.WaitGroup) {
	rows, err := connPool.Query(credentials, "select count(*) from test")
	if err != nil {
		println(err)
	}
	defer rows.Close()

	for rows.Next() {
		var num string
		if err = rows.Scan(&num); err != nil {
			panic(err)
		}
		log.Print("Number of rows is: " + num)
	}
	group.Done()
}
