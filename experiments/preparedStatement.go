package experiments

import (
	"fmt"
	"memsql-conn-pool/sql"
	"sync"
)

type prepareStatement struct {

}

func (ps*prepareStatement) FillData(db*sql.DB)error{
	fmt.Println("start")
	wg := sync.WaitGroup{}
	totalRows := 500_000
	threadsCount:=500

	stat, err:=db.Prepare("insert into test values (?)")
	if err!=nil{
		return err
	}
	defer stat.Close()

	for i:=0; i < threadsCount; i++{
		wg.Add(1)
		go ps.writeMessagesToTest(stat, totalRows/threadsCount, &wg)
	}

	wg.Wait()
	fmt.Println("end")
	return nil
}

func (ps*prepareStatement) writeMessagesToTest(stmt*sql.Stmt, amount int, wg *sync.WaitGroup) {
	for i := 0; i < amount; i++{
		_, err:=stmt.Exec(i)
		if err != nil{
			panic(err)
		}
	}
	wg.Done()
}
