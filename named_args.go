package cpool

import (
	"cpool/driver"
	"sort"
)

// Register makes a database driver available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(name string, driver driver.Driver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	if driver == nil {
		panic("sql: Register driver is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("sql: Register called twice for driver " + name)
	}
	drivers[name] = driver
}

func unregisterAllDrivers() {
	driversMu.Lock()
	defer driversMu.Unlock()
	// For tests.
	drivers = make(map[string]driver.Driver)
}

// Drivers returns a sorted list of the names of the registered drivers.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	list := make([]string, 0, len(drivers))
	for name := range drivers {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}

// A NamedArg is a named argument. NamedArg values may be used as
// arguments to Query or Exec and bind to the corresponding named
// parameter in the SQL statement.
//
// For a more concise way to create NamedArg values, see
// the Named function.
type NamedArg struct {
	_Named_Fields_Required struct{}

	// Name is the name of the parameter placeholder.
	//
	// If empty, the ordinal position in the argument list will be
	// used.
	//
	// Name must omit any symbol prefix.
	Name string

	// Value is the value of the parameter.
	// It may be assigned the same value types as the query
	// arguments.
	Value interface{}
}

// Named provides a more concise way to create NamedArg values.
//
// Example usage:
//
//     connPool.ExecContext(ctx, `
//         delete from Invoice
//         where
//             TimeCreated < @end
//             and TimeCreated >= @start;`,
//         sql.Named("start", startTime),
//         sql.Named("end", endTime),
//     )
func Named(name string, value interface{}) NamedArg {
	// This method exists because the go1compat promise
	// doesn't guarantee that structs don't grow more fields,
	// so unkeyed struct literals are a vet error. Thus, we don't
	// want to allow sql.NamedArg{name, value}.
	return NamedArg{Name: name, Value: value}
}
