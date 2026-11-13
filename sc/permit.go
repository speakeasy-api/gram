package sc

import (
	"context"
	"errors"

	"golang.org/x/sync/semaphore"
)

// Permit is the value of a resource declared with
// [StateBuilder.HoldsPermit]: one unit of a shared budget.
type Permit struct{}

// ErrNoPermit is the failure of a permit's acquisition when the budget has
// no unit free.
var ErrNoPermit = errors.New("no permit is free")

// HoldsPermit makes the state hold one unit of sem for as long as it is
// active. Entering the state tries to take a unit without waiting: OnAcquired
// fires when it got one, and OnError, with [ErrNoPermit], when none was
// free. Leaving the state, or closing the instance, gives the unit back.
//
// Sharing one semaphore between instances caps how many of them can be in
// the state at once: a pod-wide limit on requests in flight, or on hosts
// running. Route OnError back to a state that tries again later; that is a
// skip, where [LimitServices] would wait.
//
// Declare the permit before the state's other resources, which are then
// acquired only once the permit is held.
func (s *StateBuilder[S, C]) HoldsPermit(name string, sem *semaphore.Weighted) *Resource[S, C, Permit] {
	acquire, release := permitFuncs[S, C](sem)

	return s.Holds(name, acquire, release)
}

// Permit binds the resource named name, declared with
// HoldsRequired[sc.Permit], to one unit of sem, as
// [StateBuilder.HoldsPermit] does.
func (b *Binder[S, C]) Permit(name string, sem *semaphore.Weighted) {
	acquire, release := permitFuncs[S, C](sem)
	b.Resource(name, acquire, release)
}

func permitFuncs[S comparable, C any](sem *semaphore.Weighted) (func(context.Context, C) (Permit, error), func(context.Context, Permit, Leave[S])) {
	acquire := func(context.Context, C) (Permit, error) {
		if !sem.TryAcquire(1) {
			return Permit{}, ErrNoPermit
		}

		return Permit{}, nil
	}

	release := func(context.Context, Permit, Leave[S]) { sem.Release(1) }

	return acquire, release
}
