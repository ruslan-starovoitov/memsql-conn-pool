package cpool

//connRequest добавляется в connRequests когда ConnPool не может использовать закэшированные соединения
type connRequest struct {
	//Ссылка на пул, который сделал запрос
	connPool *ConnPool
	//Канал, который пул слушает
	responce chan connCreationResponse
}
