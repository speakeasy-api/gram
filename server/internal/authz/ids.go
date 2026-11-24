package authz

import (
	"errors"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// This file holds the hand-written halves of the generated id types
// (ids_gen.go) and typed refs (refs_gen.go). A typed ref takes one id type per
// argument, so a raw string, a bare uuid or an id of another kind does not
// compile. An id that cannot name a resource is kept as absent, and every
// check built from it is refused.

// idFromUUID is the id an id type holds for a uuid. The nil uuid names no
// resource, so it is absent.
func idFromUUID(id uuid.UUID) Option[string] {
	if id == uuid.Nil {
		return None[string]()
	}
	return Some(id.String())
}

// idFromString is the id an id type holds for a string. Absent: the empty
// string, the nil uuid written out, as idFromUUID treats it, the wildcard,
// which only grants may carry, a reserved marker such as $represented_user,
// and anything containing whitespace.
func idFromString(id string) Option[string] {
	if id == "" || strings.EqualFold(id, uuid.Nil.String()) || id == WildcardResource || strings.HasPrefix(id, "$") || strings.ContainsFunc(id, unicode.IsSpace) {
		return None[string]()
	}
	return Some(id)
}

// idValue is the string a check carries for an id: its value, or the empty
// string for an absent one, which validateInput and validateCheckDimensions
// refuse.
func idValue(id Option[string]) string {
	value, ok := id.Get()
	if !ok {
		return ""
	}
	return value
}

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

// newRefCheck is the helper every generated ref method in refs_gen.go builds
// its check with.
func newRefCheck(scope Scope, resourceID string, dimensions map[string]string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}

// openKeys is one scope's open keys as the registry declares them: the grant
// keys each kind of check may leave out. scopeOpenKeys holds them and
// Check.mayOmit reads them.
type openKeys struct {
	Probe          []string
	Connection     []string
	PolicyAudience []string
}
