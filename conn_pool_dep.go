package cpool

import (
	"fmt"
	"log"
)

// depSet is a finalCloser's outstanding dependencies
type depSet map[interface{}]bool // set of true bools

// The finalCloser interface is used by (*ConnPool).addDep and related
// dependency reference counting.
type finalCloser interface {
	// finalClose is called when the reference count of an object
	// goes to zero. (*ConnPool).mu is not held while calling it.
	finalClose() error
}

// addDep notes that x now depends on dep, and x's finalClose won't be
// called until all of x's dependencies are removed with removeDep.
func (connPool *ConnPool) addDep(x finalCloser, dep interface{}) {
	connPool.poolFacade.mu.Lock()
	defer connPool.poolFacade.mu.Unlock()
	connPool.addDepLocked(x, dep)
}

func (connPool *ConnPool) addDepLocked(x finalCloser, dep interface{}) {
	if connPool.dep == nil {
		connPool.dep = make(map[finalCloser]depSet)
	}
	xdep := connPool.dep[x]
	if xdep == nil {
		xdep = make(depSet)
		connPool.dep[x] = xdep
	}
	xdep[dep] = true
}

// removeDep notes that x no longer depends on dep.
// If x still has dependencies, nil is returned.
// If x no longer has any dependencies, its finalClose method will be
// called and its error value will be returned.
func (connPool *ConnPool) removeDep(x finalCloser, dep interface{}) error {
	connPool.poolFacade.mu.Lock()
	fn := connPool.removeDepLocked(x, dep)
	connPool.poolFacade.mu.Unlock()
	return fn()
}

func (connPool *ConnPool) removeDepLocked(x finalCloser, dep interface{}) func() error {
	log.Println("removeDepLocked start")
	xdep, ok := connPool.dep[x]
	if !ok {
		panic(fmt.Sprintf("unpaired removeDep: no deps for %T", x))
	}

	l0 := len(xdep)
	delete(xdep, dep)

	switch len(xdep) {
	case l0:
		// Nothing removed. Shouldn't happen.
		panic(fmt.Sprintf("unpaired removeDep: no %T dep on %T", dep, x))
	case 0:
		// No more dependencies.
		log.Println("removeDepLocked No more dependencies")
		delete(connPool.dep, x)
		return x.finalClose
	default:
		// Dependencies remain.
		log.Println("removeDepLocked Dependencies remain")
		return func() error { return nil }
	}
}
