package authz

import (
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// This file holds the hand-written half of the id types generated in
// ids_gen.go. A typed ref takes one id type per argument, so a raw string, a
// bare uuid or an id of another kind does not compile. An id that cannot name
// a resource is kept as absent, and every check built from it is refused.

// idFromUUID is the id an id type holds for a uuid. The nil uuid names no
// resource, so it is absent.
func idFromUUID(id uuid.UUID) Option[string] {
	if id == uuid.Nil {
		return None[string]()
	}
	return Some(id.String())
}

// idFromString is the id an id type holds for a string. Absent: the empty
// string, the wildcard, which only grants may carry, a reserved marker such
// as $represented_user, and anything containing whitespace.
func idFromString(id string) Option[string] {
	if id == "" || id == WildcardResource || strings.HasPrefix(id, "$") || strings.ContainsFunc(id, unicode.IsSpace) {
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
