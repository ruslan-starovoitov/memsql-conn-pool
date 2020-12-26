package experiments

import (
	"fmt"
	"memsql-conn-pool/sql"
	"strconv"
)

type driverNamesReader struct {
}

func (dnr *driverNamesReader) readDriverNames() {
	fmt.Println("Printing driver names:")
	for index, name := range sql.Drivers() {
		fmt.Println("index = " + strconv.Itoa(index) + " name = " + name)
	}
	fmt.Println("Printing driver names ended")
}
