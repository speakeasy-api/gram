package authz

// The snapshot parts that depend on the current API. To compare with an older
// evaluator, copy decision_snapshot_cases_test.go there and write this file
// for its API.

func snapshotMCPServer(id, project string) MCPServerRef {
	return MCPServer(MCPServerIDFrom(id), ProjectIDFrom(project))
}

func snapshotRawCheck(scope Scope, kind, id string, dimensions map[string]string) Check {
	return Check{Scope: scope, ResourceKind: kind, ResourceID: id, Dimensions: dimensions, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}
