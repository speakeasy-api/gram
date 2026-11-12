package authz

import (
	"reflect"

	"github.com/google/uuid"
)

// This file holds the hand-written half of the typed requests generated in
// refs_gen.go: the helper every ref method builds its check with, and the
// requests that need more than a resource and an action.

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

// MatchMode is how an allow grant's selector is compared with a check.
type MatchMode int

const (
	// MatchLoose skips grant keys the check does not carry. It is today's
	// default for every resource type except auth clients.
	MatchLoose MatchMode = iota
	// MatchStrict requires every grant key to be present in the check.
	MatchStrict
)

func newRefCheck(scope Scope, resourceID string, dimensions map[string]string, match MatchMode) Check {
	mode := selectorMatchNormal
	if match == MatchStrict {
		mode = selectorMatchStrict
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: mode, representedUser: "", probe: false, anyOf: nil}
}

// ToolCall requests mcp:connect for calling one tool on the server. A tool
// or disposition that is empty is left out of the check (see MCPToolCallCheck).
func (r MCPServerRef) ToolCall(tool, disposition string) Check {
	return MCPToolCallCheck(r.id, MCPToolCallDimensions{Tool: tool, Disposition: disposition, ProjectID: r.project})
}
