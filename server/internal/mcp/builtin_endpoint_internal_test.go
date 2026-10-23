package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestBuiltinEndpointIdentity(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	id := builtinmcp.ID(projectID, "slack")
	endpoint := &ResolvedMcpEndpoint{BuiltinID: uuid.NullUUID{UUID: id, Valid: true}, OrganizationID: "org_example", ProjectID: projectID, UserSessionIssuerID: uuid.New(), RouteBase: mcproute.Builtin, Slug: "example/project/slack"}
	root, err := endpoint.RootURL("https://mcp.example")
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example/mcp/builtin/example/project/slack", root)
	protected, err := endpoint.ProtectedResourceURL("https://mcp.example")
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example/.well-known/oauth-protected-resource/mcp/builtin/example/project/slack", protected)
	target, ok := agentAuthorizationTarget(endpoint)
	require.True(t, ok)
	require.Equal(t, id, target.MCPResourceID)
	require.Equal(t, id, endpoint.connectResourceID())
	require.Equal(t, "builtin_mcp:"+id.String(), endpointToolSelectionResource(endpoint))
	_, err = toolfilter.NewSessionSelection(endpointToolSelectionResource(endpoint), uuid.New(), []toolfilter.AllowEntry{})
	require.NoError(t, err)
	ref, err := endpoint.EndpointRef(t.Context(), nil, "https://mcp.example")
	require.NoError(t, err)
	require.NoError(t, endpoint.ValidateRef(ref))
	ref.BuiltinID.UUID = uuid.New()
	require.Error(t, endpoint.ValidateRef(ref))
	require.False(t, endpoint.McpServerID.Valid)
	require.False(t, endpoint.ToolsetID.Valid)
}

func TestBuiltinSlugValidation(t *testing.T) {
	t.Parallel()
	require.True(t, validBuiltinSlug("example/project/slack"))
	for _, slug := range []string{"project/slack", "example/project/slack/extra", "/project/slack", "example//slack", "example/project/unknown"} {
		require.False(t, validBuiltinSlug(slug), slug)
	}
}

func TestBuiltinConsentRequiresExplicitAgentGrant(t *testing.T) {
	t.Parallel()
	projectID, serverID := uuid.New(), uuid.New()
	target := AgentAuthorizationTarget{Builtin: true, Scope: authz.ScopeMCPConnect, ProjectID: projectID, MCPResourceID: serverID}
	broad := authz.Grant{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: authz.WildcardResource, authz.SelectorKeyProjectID: projectID.String()}}
	allowed, err := target.agentPolicyConnect([]authz.Grant{broad})
	require.NoError(t, err)
	require.False(t, allowed)
	broad.Selector[authz.SelectorKeyResourceID] = serverID.String()
	allowed, err = target.agentPolicyConnect([]authz.Grant{broad})
	require.NoError(t, err)
	require.True(t, allowed)
	blocked := authz.Grant{Scope: authz.ScopeMCPBlockedConnect, Selector: broad.Selector}
	allowed, err = target.agentPolicyConnect([]authz.Grant{broad, blocked})
	require.NoError(t, err)
	require.False(t, allowed, "an exact allow never wins over a block")
}

func TestBuiltinHasNoRemoteConnectionPrerequisite(t *testing.T) {
	t.Parallel()
	service := &Service{}
	endpoint := &ResolvedMcpEndpoint{RouteBase: mcproute.Builtin}
	require.NoError(t, service.checkEndpointAgentConnections(t.Context(), endpoint, urn.NewAgentSubject(uuid.New())))
}
