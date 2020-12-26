package experiments

import (
	"fmt"
	"memsql-conn-pool/sql"
	"strconv"
	"sync"
)

type dataFiller struct {
}

func (df *dataFiller) FillData(db *sql.DB) error {
	fmt.Println("start")
	wg := sync.WaitGroup{}
	totalRows := 9_000_000
	threadsCount := 50

	for i := 0; i < threadsCount; i++ {
		wg.Add(1)
		go df.writeMessagesToTest(db, totalRows/threadsCount, &wg)
	}

	wg.Wait()
	fmt.Println("end")
	return nil
}

func (df *dataFiller) writeMessagesToTest(db *sql.DB, amount int, wg *sync.WaitGroup) {
	for i := 0; i < amount; i++ {
		statement := "insert into test (message) value (\"" + strconv.Itoa(i) + " large large message\");"
		_, err := db.Exec(statement)
		//stats:=db.Stats()
		//statsStr := "in use = "+strconv.Itoa(stats.InUse)+" idle = "+strconv.Itoa(stats.Idle)+
		//	" open connections = "+strconv.Itoa(stats.OpenConnections)
		//fmt.Println(statsStr)
		if err != nil {
			panic(err)
		}
	}
	wg.Done()
}
