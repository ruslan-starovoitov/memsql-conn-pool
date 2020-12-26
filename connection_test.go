package memsql_conn_pool

import (
	_ "github.com/go-sql-driver/mysql"
)

//var testCases = []struct {
//	auth     Auth
//	Database string
//	name     string
//}{
//	{
//		name:     "root login is ok",
//		auth:     Auth{Username: "root", Password: "RootPass1"},
//		Database: "hellomemsql",
//	},
//}
//
//func TestConnectionFactory_CreateConnection(t *testing.T) {
//	var connFactory = connectionFactory{}
//	for _, tt := range testCases {
//		t.Run(tt.name, func(t *testing.T) {
//			db, err := connFactory.CreateConnection(tt.auth, tt.Database)
//			require.NoError(t, err)
//			assert.NotNil(t, db)
//			err = db.Close()
//			require.NoError(t, err)
//		})
//	}
//}
