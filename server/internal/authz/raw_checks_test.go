package authz

// Engine tests build checks the typed refs refuse to: a check with no project
// dimension, an arbitrary scope over a server, or a tool call missing parts.
// These helpers keep those checks expressible in this package's tests only;
// code outside authz builds every check from a typed ref.

// mcpToolCallDimensions carries the parts of an MCP tool call. An empty field
// is left out of the check.
type mcpToolCallDimensions struct {
	Tool        string
	Disposition string
	ProjectID   string
}

// mcpToolCallCheck is a tool call on toolsetID with the given dimensions.
func mcpToolCallCheck(toolsetID string, dims mcpToolCallDimensions) Check {
	dimensions := map[string]string{}
	if dims.Disposition != "" {
		dimensions[SelectorKeyDisposition] = dims.Disposition
	}
	if dims.Tool != "" {
		dimensions[SelectorKeyTool] = dims.Tool
	}
	if dims.ProjectID != "" {
		dimensions[SelectorKeyProjectID] = dims.ProjectID
	}
	return Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: toolsetID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationToolCall}
}

// mcpCheck is scope on resourceID, with projectID as a dimension when it is
// not empty. A connect check is a connection.
func mcpCheck(scope Scope, resourceID, projectID string) Check {
	var dimensions map[string]string
	if projectID != "" {
		dimensions = map[string]string{SelectorKeyProjectID: projectID}
	}
	check := Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
	if isConnectScope(scope) {
		return check.asConnection()
	}
	return check
}

// assistantCheck is scope on resourceID in projectID.
func assistantCheck(scope Scope, resourceID, projectID string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}
