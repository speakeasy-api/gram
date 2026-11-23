package authz

// RecordedCheck rebuilds a check that was stored earlier, such as the denied
// request an access challenge keeps, from its scope, resource and dimensions.
// A connect check that names a tool or a disposition was a tool call, so it is
// rebuilt as one; anything else is rebuilt as an ordinary check. A recorded
// check the evaluator would refuse today, such as a view check that names a
// tool, stays invalid.
func RecordedCheck(scope Scope, resourceKind, resourceID string, dimensions map[string]string) Check {
	check := Check{Scope: scope, ResourceKind: resourceKind, ResourceID: resourceID, Dimensions: dimensions, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, exact: false}
	_, hasTool := dimensions[SelectorKeyTool]
	_, hasDisposition := dimensions[SelectorKeyDisposition]
	if (hasTool || hasDisposition) && isConnectScope(scope) {
		check.operation = operationToolCall
	}
	return check
}
