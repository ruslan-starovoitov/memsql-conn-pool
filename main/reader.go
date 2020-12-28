package main

import (
	"fmt"
	cpool "memsql-conn-pool"
	"sync"
)

type reader struct {
}

func (reader) Read(credentials cpool.Credentials, connPool *cpool.PoolManager, group *sync.WaitGroup) {
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
		fmt.Println("Number of rows is: " + num)
		fmt.Println()
	}
	group.Done()
}
