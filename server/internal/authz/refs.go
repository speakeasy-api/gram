package authz

// This file holds the hand-written half of the typed requests generated in
// refs_gen.go: the helper every ref method builds its check with.

func newRefCheck(scope Scope, resourceID string, dimensions map[string]string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}
