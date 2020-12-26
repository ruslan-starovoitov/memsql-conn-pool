package experiments

import (
	_ "memsql-conn-pool/mysql"
	"memsql-conn-pool/sql"
)

func main() {
	err := run()
	if err != nil {
		panic(err)
	}
}

func run() error {
	db, err := createConnectionPool()
	if err != nil {
		return err
	}
	//err=truncateTable(db)
	//if err!=nil{
	//	return err
	//}
	//err=fillData(db)
	//if err!=nil{
	//	return err
	//}
	err = readData(db)
	if err != nil {
		return err
	}
	return nil
}

func createConnectionPool() (*sql.DB, error) {
	db, err := sql.Open("mysql", "root:RootPass1@/hellomemsql?interpolateParams=true")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(30)
	return db, err
}

func truncateTable(db *sql.DB) error {
	_, err := db.Exec("truncate table test;")
	return err
}

func fillData(db *sql.DB) error {
	df := dataFiller{}
	return df.FillData(db)
}

func readData(db *sql.DB) error {
	dr := dataReader{}
	return dr.readTest(db)
}

//func runPreparedStatement(db *sql.DB) error {
//	ps:=prepareStatement{}
//	return ps.FillData(db)
//}
