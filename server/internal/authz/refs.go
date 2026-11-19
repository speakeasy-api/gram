package authz

import (
	"reflect"

	"github.com/google/uuid"
)

// This file holds the hand-written half of the typed requests generated in
// refs_gen.go: the id types refs accept and the helper every ref method
// builds its check with.

// ID is the id a typed ref takes: a resource's uuid, or its id as a string.
// Ref constructors are generic over it so callers pass ids as they hold them.
type ID interface {
	~string | uuid.UUID
}

func idString[T ID](id T) string {
	if u, ok := any(id).(uuid.UUID); ok {
		return u.String()
	}
	return reflect.ValueOf(id).String()
}

func newRefCheck(scope Scope, resourceID string, dimensions map[string]string, match MatchMode) Check {
	mode := selectorMatchNormal
	if match == MatchStrict {
		mode = selectorMatchStrict
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: mode, representedUser: "", probe: false, anyOf: nil}
}
