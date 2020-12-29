package memsql_conn_pool_tests

import (
	"fmt"
	cpool "memsql-conn-pool"
	mysql "memsql-conn-pool/mysql"
	"strings"
	"time"
)

var initCredentials = cpool.Credentials{
	Username: "root",
	Password: "RootPass1",
	Database: "information_schema",
}


const (
	userAlreadyExistsErrorNumber = 1924
)

func init() {
	pm := cpool.NewPoolFacade(1, time.Second)

	sql := `
	CREATE DATABASE IF NOT EXISTS hellomemsql;
	CREATE TABLE IF NOT EXISTS hellomemsql.test (message text NOT NULL);	
	TRUNCATE TABLE hellomemsql.test;
	CREATE DATABASE IF NOT EXISTS hellomemsql1;
	CREATE TABLE IF NOT EXISTS hellomemsql1.test (message text NOT NULL);	
	TRUNCATE TABLE hellomemsql1.test;
	CREATE DATABASE IF NOT EXISTS hellomemsql2;
	CREATE TABLE IF NOT EXISTS hellomemsql2.test (message text NOT NULL);	
	TRUNCATE TABLE hellomemsql2.test;

	create user 'user1'@'%' identified by 'pass1';
	create user 'user2'@'%' identified by 'pass2';
	create user 'user3'@'%' identified by 'pass3';
	GRANT ALL PRIVILEGES ON hellomemsql1.* TO 'user1'@'%' WITH GRANT OPTION;
	GRANT ALL PRIVILEGES ON hellomemsql1.* TO 'user2'@'%' WITH GRANT OPTION;
	GRANT ALL PRIVILEGES ON hellomemsql1.* TO 'user3'@'%' WITH GRANT OPTION;	


	create user 'user4'@'%' identified by 'pass4';
	create user 'user5'@'%' identified by 'pass5';
	create user 'user6'@'%' identified by 'pass6';
	GRANT ALL PRIVILEGES ON hellomemsql2.* TO 'user4'@'%' WITH GRANT OPTION;
	GRANT ALL PRIVILEGES ON hellomemsql2.* TO 'user5'@'%' WITH GRANT OPTION;
	GRANT ALL PRIVILEGES ON hellomemsql2.* TO 'user6'@'%' WITH GRANT OPTION;

	FLUSH PRIVILEGES;`
	statements := strings.Split(sql, ";")
	tx, err := pm.BeginTx(initCredentials)
	for _, statement := range statements {
		fmt.Print(statement)
		if len(statement)==0{
			continue
		}
		_, err = tx.Exec(statement)
		if err != nil {
			//Ignore User already exists error
			if value, ok := err.(*mysql.MySQLError); ok {
				if value.Number == userAlreadyExistsErrorNumber {
					continue
				}
			}

			panic(err)
		}
	}

	err = tx.Commit()
	if err != nil {
		panic(err)
	}
}
