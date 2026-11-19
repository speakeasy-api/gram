package authz

import "errors"

// ErrEmptyValue reports a comparison in which one side has no value. An
// absent value is never a match and never a quiet "no": the caller must
// refuse the request.
var ErrEmptyValue = errors.New("authz: comparing an empty value")

// Option is a value that may be absent. The zero Option is absent.
type Option[T comparable] struct {
	value T
	set   bool
}

// Some is the Option holding v.
func Some[T comparable](v T) Option[T] {
	return Option[T]{value: v, set: true}
}

// None is the absent Option.
func None[T comparable]() Option[T] {
	var zero T
	return Option[T]{value: zero, set: false}
}

// Get returns the value and whether there is one.
func (o Option[T]) Get() (T, bool) {
	return o.value, o.set
}

// Equals reports whether both sides hold the same value. It fails with
// ErrEmptyValue when either side is absent, so absence can never be read as
// a match or as an ordinary mismatch.
func (o Option[T]) Equals(other Option[T]) (bool, error) {
	if !o.set || !other.set {
		return false, ErrEmptyValue
	}
	return o.value == other.value, nil
}
