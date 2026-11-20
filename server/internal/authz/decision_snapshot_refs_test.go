package authz

// The parts of the decision snapshot that depend on this branch's API. To
// compare with another version of the evaluator, copy
// decision_snapshot_cases_test.go there and write this file for its API.

func snapshotMCPServer(id, project string) MCPServerRef {
	return MCPServer(MCPServerIDFrom(id), ProjectIDFrom(project))
}

func snapshotRawCheck(scope Scope, kind, id string, dimensions map[string]string) Check {
	return Check{Scope: scope, ResourceKind: kind, ResourceID: id, Dimensions: dimensions, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}
