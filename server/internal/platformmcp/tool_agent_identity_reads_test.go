package platformmcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestAgentIdentityReadToolsContract(t *testing.T) {
	t.Parallel()

	reg := newRegistrar(newTestMCPServer())
	registerAgentIdentityReadTools(reg, nil)
	for _, name := range []string{getAgentMCPAccessToolName, listAgentAuthorizationChecksToolName} {
		descriptor := descriptorByName(t, reg, name)
		require.Equal(t, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeNone, DiscoveryScopes: nil}, descriptor.Meta, "%s: an agent's owner need not be an administrator, and agents are not project tools", name)
		require.True(t, descriptor.Annotations.ReadOnlyHint, name)
		require.False(t, *descriptor.Annotations.DestructiveHint, name)
		require.False(t, *descriptor.Annotations.OpenWorldHint, name)

		var schema struct {
			Required []string `json:"required"`
		}
		require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
		require.Equal(t, []string{"agent_id"}, schema.Required, name)

		_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"agent_id":"11111111-1111-4111-8111-111111111111"}`))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal, name)
		require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`, name)
		require.Contains(t, refusal.Payload, `"feature":"agent_identity_reads"`, name)
	}
}

func TestAgentIdentityReadRefusalsDoNotRevealAgents(t *testing.T) {
	t.Parallel()

	for err, code := range map[error]string{
		ErrAgentNotAvailable:        `"code":"not_found"`,
		ErrAgentChecksForbidden:     `"code":"permission_denied"`,
		ErrAgentChecksWindowInvalid: `"code":"invalid_request"`,
		ErrForbidden:                `"code":"permission_denied"`,
		ErrUnavailable:              `"code":"feature_unavailable"`,
	} {
		result, ok := agentIdentityReadToolResult(err)
		require.True(t, ok, err)
		require.True(t, result.IsError, err)
		text, isText := result.Content[0].(*mcp.TextContent)
		require.True(t, isText, err)
		require.Contains(t, text.Text, code, err)
	}
	_, ok := agentIdentityReadToolResult(json.Unmarshal([]byte("{"), &struct{}{}))
	require.False(t, ok, "an unexpected failure is not dressed up as a refusal")
}

func TestOffersPersonalSignIn(t *testing.T) {
	t.Parallel()

	personal := &types.AuthClient{Enabled: true, ClientType: "remote_session"}
	for name, tc := range map[string]struct {
		auth *types.McpServerAuthentication
		want bool
	}{
		"no authentication read":         {auth: nil, want: false},
		"no auth clients":                {auth: &types.McpServerAuthentication{Status: "no_auth_required"}, want: false},
		"setup incomplete":               {auth: &types.McpServerAuthentication{Status: "setup_incomplete"}, want: true},
		"shared service account only":    {auth: &types.McpServerAuthentication{Status: "configured", Clients: []*types.AuthClient{{Enabled: true, ClientType: "service_account"}}}, want: false},
		"each person's account":          {auth: &types.McpServerAuthentication{Status: "configured", Clients: []*types.AuthClient{personal}}, want: true},
		"disabled each person's account": {auth: &types.McpServerAuthentication{Status: "configured", Clients: []*types.AuthClient{{Enabled: false, ClientType: "remote_session"}}}, want: false},
	} {
		require.Equal(t, tc.want, offersPersonalSignIn(tc.auth), name)
	}
}

func TestPassesCallerCredentialThrough(t *testing.T) {
	t.Parallel()

	require.False(t, passesCallerCredentialThrough(nil))
	require.False(t, passesCallerCredentialThrough(&types.McpServerAuthentication{Clients: []*types.AuthClient{{Enabled: false, ClientType: "pass_through"}}}))
	require.True(t, passesCallerCredentialThrough(&types.McpServerAuthentication{Clients: []*types.AuthClient{{Enabled: true, ClientType: "pass_through"}}}))
}

func TestRemoteSessionEnded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) pgtype.Timestamptz { return pgtype.Timestamptz{Time: now.Add(d), Valid: true} }
	refresh := pgtype.Text{String: "encrypted", Valid: true}
	for name, tc := range map[string]struct {
		session remotesessionsrepo.RemoteSession
		want    bool
	}{
		"no expiry recorded":                {session: remotesessionsrepo.RemoteSession{}, want: false},
		"access token expired, no refresh":  {session: remotesessionsrepo.RemoteSession{AccessExpiresAt: at(-time.Minute)}, want: true},
		"access token expired, refreshes":   {session: remotesessionsrepo.RemoteSession{AccessExpiresAt: at(-time.Minute), RefreshTokenEncrypted: refresh}, want: false},
		"refresh token expired":             {session: remotesessionsrepo.RemoteSession{AccessExpiresAt: at(-time.Minute), RefreshTokenEncrypted: refresh, RefreshExpiresAt: at(-time.Second)}, want: true},
		"refresh lapsed, access still good": {session: remotesessionsrepo.RemoteSession{AccessExpiresAt: at(time.Minute), RefreshTokenEncrypted: refresh, RefreshExpiresAt: at(-time.Second)}, want: false},
		"authorization deadline passed":     {session: remotesessionsrepo.RemoteSession{AccessExpiresAt: at(time.Hour), RefreshTokenEncrypted: refresh, AuthorizationExpiresAt: at(-time.Second)}, want: true},
	} {
		require.Equal(t, tc.want, remoteSessionEnded(tc.session, now), name)
	}
}
