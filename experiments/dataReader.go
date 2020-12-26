package experiments

import (
	"fmt"
	"memsql-conn-pool/sql"
	"runtime"
)

type dataReader struct {

}

func (dr*dataReader)readTest(db*sql.DB) error{

	rows, err := db.Query("select * from test")
	if err != nil{
		return err
	}

	count:=0
	for rows.Next(){
		var message string
		if err = rows.Scan(&message); err==nil{
			count++
			//fmt.Println(strconv.Itoa(count)+" "+message)
		}
		if count%100==0{
			fmt.Println()
			PrintMemUsage()
		}
	}
	return nil
}

// PrintMemUsage outputs the current, total and OS memory being used. As well as the number
// of garage collection cycles completed.
func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats

	fmt.Printf("\tAlloc = %v MiB", bToMb(m.Alloc))
	fmt.Printf("\tSys = %v MiB", bToMb(m.Sys))
	fmt.Printf("\tHeapAlloc = %v MiB", bToMb(m.HeapAlloc))

}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
