package cpool

import "errors"

var unableToGetPool = errors.New("unable get connection pool from concurrent map")
