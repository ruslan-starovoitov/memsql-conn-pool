package memsql_conn_pool

import "memsql-conn-pool/sql"

type IPool interface {
	Query(credentials Credentials, sql string) (*sql.Rows, error)
	Exec(credentials Credentials, sql string) (sql.Result, error)
	ClosePool()
}
