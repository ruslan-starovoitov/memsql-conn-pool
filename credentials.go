package cpool

//TODO хранит уникальные данные об подключении
type Credentials struct {
	Username string
	Password string
	Database string
}

func GetDataSourceName(credentials Credentials) string {
	return credentials.Username + ":" + credentials.Password + "@/" + credentials.Database + "?interpolateParams=true"
}

func (cr *Credentials) GetId() string {
	return cr.Username + cr.Password + cr.Database
}
