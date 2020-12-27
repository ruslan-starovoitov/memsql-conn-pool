package main

import (
	"fmt"
	pool "memsql-conn-pool"
	"time"
)

func main() {
	err := run()
	if err != nil {
		panic(err)
	}
}

var credentials = pool.Credentials{
	Username: "root",
	Password: "RootPass1",
	Database: "hellomemsql",
}

func run() error {
	connPool := pool.NewPool(100, time.Minute)
	rows, err := connPool.Query(credentials, "select count(*) from test")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var num string
		if err = rows.Scan(&num); err != nil {
			return err
		}
		fmt.Println("Number of rows is: " + num)
		fmt.Println()
	}

	return nil
}
