package main

import (
	_ "cpool/mysql"
)

func main() {
	ex := Example1{}
	ex.Run()
}

//type Example2 struct {
//}
//
//func (Example2) Run() {
//	var mutex1 sync.Mutex
//	//var mutex2 sync.Mutex
//
//	mutex1.Lock()
//	mutex1.Lock()
//
//	mutex1.Unlock()
//	//mutex1.Unlock()
//}
