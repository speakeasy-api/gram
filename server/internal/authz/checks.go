package authz

// MCPToolCallDimensions carries the typed attributes of an MCP tool call.
// Zero-value fields are omitted from the check dimensions.
type MCPToolCallDimensions struct {
	Tool        string
	Disposition string
	ProjectID   string
}

// MCPToolCallCheck builds a Check for an MCP tool call with the given dimensions.
func MCPToolCallCheck(toolsetID string, dims MCPToolCallDimensions) Check {
	dimensions := map[string]string{}
	if dims.Tool != "" {
		dimensions[SelectorKeyTool] = dims.Tool
	}
	if dims.Disposition != "" {
		dimensions[SelectorKeyDisposition] = dims.Disposition
	}
	if dims.ProjectID != "" {
		dimensions[SelectorKeyProjectID] = dims.ProjectID
	}
	return Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: toolsetID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, expanded: false}
}

// MCPCheck builds a Check for an MCP scope (read/write/connect) with project_id
// injected as a dimension so project-scoped grants can match.
func MCPCheck(scope Scope, resourceID, projectID string) Check {
	var dimensions map[string]string
	if projectID != "" {
		dimensions = map[string]string{SelectorKeyProjectID: projectID}
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, expanded: false}
}

type RiskPolicyBypassDimensions struct {
	ServerURL      string
	ServerIdentity string
}

func RiskPolicyBypassCheck(policyID string, dims RiskPolicyBypassDimensions) Check {
	var dimensions map[string]string
	if dims.ServerURL != "" {
		dimensions = map[string]string{}
		dimensions[SelectorKeyServerURL] = dims.ServerURL
	}
	if dims.ServerIdentity != "" {
		if dimensions == nil {
			dimensions = map[string]string{}
		}
		dimensions[SelectorKeyServerIdentity] = dims.ServerIdentity
	}
	return Check{Scope: ScopeRiskPolicyBypass, ResourceKind: "", ResourceID: policyID, Dimensions: dimensions, selectorMatch: selectorMatchStrict, expanded: false}
}
