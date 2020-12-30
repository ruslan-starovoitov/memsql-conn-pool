package cpool

type RowsWrapper struct {
	Rows
	releasedChan chan<- struct{}
}

func (r *RowsWrapper) Close() {
	r.Close()
	r.releasedChan <- struct{}{}
}
