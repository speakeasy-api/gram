package authz

// This file holds the hand-written half of the typed requests generated in
// refs_gen.go: the helper every ref method builds its check with.

func newRefCheck(scope Scope, resourceID string, dimensions map[string]string, match MatchMode) Check {
	mode := selectorMatchNormal
	if match == MatchStrict {
		mode = selectorMatchStrict
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: mode, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}
