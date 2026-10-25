package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// The registered tool decodes its JSON arguments, resolves the project
// through the live project:read check, and only then reaches the session
// service. That service's own gateway and project checks are covered by
// usersessions' gateway scope tests.
func TestListGatewayConnectionsToolChecksProjectAccessBeforeReading(t *testing.T) {
	t.Parallel()

	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_gateway_connections")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	_, foreignProject := seedRegistrationLifecycle(t, t.Context(), db)
	principal.Surface = SurfacePlatformMCP
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	fake := &gatewaySessionsFake{items: []*types.UserSession{{
		ID: "s-1", UserSessionIssuerID: "issuer-1", SubjectUrn: "user:u-1", SubjectType: "user", SubjectDisplayName: new("Ada Lovelace"),
		Jti: "jti-secret", IssuerSlug: "sign-in", RefreshExpiresAt: "2026-11-01T00:00:00Z",
		Upstreams: []*types.UserSessionUpstream{{IssuerSlug: "mcp.example.com", RemoteSessionID: "remote-session-1", HasRefreshToken: true}},
	}}}
	reg := newRegistrar(newTestMCPServer())
	registerGatewayConnectionsTool(reg, &GatewayConnectionsService{sessions: fake, resolveProject: NewPostgresReader(logger, db).WithAuthorization(engine).ResolveProjectRead})
	descriptor := descriptorByName(t, reg, listGatewayConnectionsToolName)
	gateway := uuid.NewString()

	member := dashboardCallerContext(t.Context(), principal, project, authz.ScopeProjectRead)
	value, err := descriptor.Invoke(member, json.RawMessage(`{
		"project_id": "`+project.ID.String()+`", "gateway_id": "`+gateway+`",
		"search": "ada", "status": "all", "servers": ["issuer-1"], "exclude_identity_types": ["apikey"],
		"last_used_older_than_seconds": 3600, "limit": 10
	}`))
	require.NoError(t, err)
	out, ok := value.(ListGatewayConnectionsOutput)
	require.True(t, ok, "%T", value)

	require.Equal(t, []string{project.ID.String(), project.ID.String()}, fake.projectIDs)
	require.Equal(t, gateway, *fake.list.MetaMcpServerID)
	require.Equal(t, "all", *fake.list.Status)
	require.Equal(t, []string{"issuer-1"}, fake.list.Servers)
	require.Equal(t, []string{"apikey"}, fake.facets.ExcludeIdentityTypes)
	require.Equal(t, 3600, *fake.facets.LastUsedOlderThan)
	require.Equal(t, 10, *fake.list.Limit)
	require.Equal(t, int64(3), out.UnattributedCount)
	require.Equal(t, "Ada Lovelace", out.Connections[0].DisplayName)
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	for _, secret := range []string{"jti", "token", "remote-session-1"} {
		require.NotContains(t, strings.ToLower(string(encoded)), secret)
	}

	for name, tc := range map[string]struct {
		grants    []authz.Scope
		projectID string
	}{
		"without project:read":     {grants: nil, projectID: project.ID.String()},
		"a project in another org": {grants: []authz.Scope{authz.ScopeProjectRead}, projectID: foreignProject.ID.String()},
	} {
		fake.list, fake.facets = nil, nil
		ctx := dashboardCallerContext(t.Context(), principal, project, tc.grants...)
		_, err := descriptor.Invoke(ctx, json.RawMessage(`{"project_id":"`+tc.projectID+`","gateway_id":"`+gateway+`"}`))
		require.ErrorIs(t, err, ErrForbidden, name)
		require.Nil(t, fake.list, "%s: no session is read", name)
		require.Nil(t, fake.facets, "%s: nothing is counted", name)
	}
}
