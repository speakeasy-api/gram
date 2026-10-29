package catalog

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// MCPServerTarget names an MCP server with the project and organization that
// contain it. Build it from the loaded server row and its project, never from
// request input alone.
func MCPServerTarget(organizationID, projectID, serverID string) Target {
	return Target{
		Resource: ResourceRef{OrganizationID: organizationID, Kind: authz.ResourceKindMCP, ID: serverID},
		Ancestors: []ResourceRef{
			{OrganizationID: organizationID, Kind: authz.ResourceKindProject, ID: projectID},
			{OrganizationID: organizationID, Kind: authz.ResourceKindOrg, ID: organizationID},
		},
	}
}

// MCPServerRead asks to view a server's settings.
func MCPServerRead(target Target) Request {
	return Request{Permission: authz.ScopeMCPRead, Target: target}
}

// MCPScopePin asks to change the scope pin of target. The pin is keyed by the
// upstream resource, so it also asks for mcp:write on every server sharing it.
// The capability response and the write both use these requests, so they
// reach the same decision.
func MCPScopePin(target Target, sharing []Target) []Request {
	requests := []Request{{Permission: authz.ScopeMCPWrite, Target: target}}
	seen := map[string]bool{target.Resource.ID: true}
	for _, other := range sharing {
		if seen[other.Resource.ID] {
			continue
		}
		seen[other.Resource.ID] = true
		requests = append(requests, Request{Permission: authz.ScopeMCPWrite, Target: other})
	}
	return requests
}

// MCPServerVisibility is who may use a server without a grant.
type MCPServerVisibility string

const (
	MCPServerPublic  MCPServerVisibility = "public"
	MCPServerPrivate MCPServerVisibility = "private"
)

// MCPServerUse returns what using a server requires: nothing for a public
// server, mcp:connect on that server for a private one. Configuring a server
// never satisfies it.
func MCPServerUse(target Target, visibility MCPServerVisibility) ([]Request, error) {
	switch visibility {
	case MCPServerPublic:
		return nil, nil
	case MCPServerPrivate:
		return []Request{{Permission: authz.ScopeMCPConnect, Target: target}}, nil
	default:
		return nil, fmt.Errorf("unrecognized mcp server visibility %q", visibility)
	}
}
