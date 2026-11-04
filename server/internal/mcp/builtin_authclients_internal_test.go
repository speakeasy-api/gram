package mcp

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestBuiltinStaticAuthEnvironment(t *testing.T) {
	t.Parallel()
	valid := func() *authClientResolution {
		return &authClientResolution{outcome: authClientResolved, clientID: uuid.New(), clientType: authClientTypeStaticCredential, headers: []proxy.ConfiguredHeader{{Name: "slack_bot_token", StaticValue: "test-secret"}, {Name: "UNRELATED", StaticValue: "not-tool-config"}}}
	}
	t.Run("allowlist", func(t *testing.T) {
		t.Parallel()
		env, err := builtinStaticAuthEnvironment("slack", valid())
		require.NoError(t, err)
		require.Equal(t, "test-secret", env.UserConfig.Get("SLACK_BOT_TOKEN"))
		require.Empty(t, env.UserConfig.Get("UNRELATED"))
		require.Empty(t, env.OAuthToken)
	})
	for _, outcome := range []authClientOutcome{authClientNoAuth, authClientGap, authClientNoAccess, authClientLegacy} {
		t.Run(string(outcome), func(t *testing.T) {
			t.Parallel()
			res := valid()
			res.outcome = outcome
			env, err := builtinStaticAuthEnvironment("slack", res)
			require.Error(t, err)
			require.Nil(t, env.UserConfig)
			require.NotContains(t, err.Error(), "test-secret")
		})
	}
	for _, typ := range []string{authClientTypePassThrough, authClientTypeRemoteSession, authClientTypeServiceAccount, authClientTypeEMA} {
		t.Run(typ, func(t *testing.T) {
			t.Parallel()
			res := valid()
			res.clientType = typ
			_, err := builtinStaticAuthEnvironment("slack", res)
			require.Error(t, err)
		})
	}
	for _, tc := range []struct {
		name    string
		headers []proxy.ConfiguredHeader
	}{
		{"empty", nil},
		{"unknown only", []proxy.ConfiguredHeader{{Name: "Authorization", StaticValue: "test-secret"}}},
		{"forwarded", []proxy.ConfiguredHeader{{Name: "SLACK_TOKEN", StaticValue: "test-secret", ValueFromRequestHeader: "X-Slack"}}},
		{"blank", []proxy.ConfiguredHeader{{Name: "SLACK_TOKEN", StaticValue: " "}}},
		{"duplicate", []proxy.ConfiguredHeader{{Name: "SLACK_TOKEN", StaticValue: "test-secret"}, {Name: "slack_token", StaticValue: "other-secret"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := valid()
			res.headers = tc.headers
			env, err := builtinStaticAuthEnvironment("slack", res)
			require.Error(t, err)
			require.Nil(t, env.UserConfig)
			require.NotContains(t, err.Error(), "test-secret")
		})
	}
	_, err := builtinStaticAuthEnvironment("slack", nil)
	require.Error(t, err)
	_, err = builtinStaticAuthEnvironment("approvals", valid())
	require.Error(t, err)
}

func TestBuiltinAuthLegacyModesDoNotResolveCredentials(t *testing.T) {
	t.Parallel()
	for _, shadow := range []bool{false, true} {
		flags := new(feature.InMemory)
		flags.SetFlag(feature.FlagAuthClientResolverShadow, "org_test", shadow)
		service := &Service{features: flags}
		// No DB, loader, selector, or usable client: legacy/shadow must touch none.
		authentication := &issuerGateAuthentication{endpoint: &ResolvedMcpEndpoint{OrganizationID: "org_test"}}
		env, handled, err := service.builtinDirectAuthEnvironment(t.Context(), authentication, "slack")
		require.NoError(t, err)
		require.False(t, handled)
		require.Nil(t, env.UserConfig)
		env, handled, err = service.builtinMemberAuthEnvironment(t.Context(), "org_test", metaMember{builtinKey: "slack"})
		require.NoError(t, err)
		require.False(t, handled)
		require.Nil(t, env.UserConfig)
	}
}

func TestBuiltinAuthModeReadFailureDoesNotAllowFallback(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "builtin_auth_mode_error")
	require.NoError(t, err)
	service := &Service{authEnforcement: authenforcement.NewGate(pool)}
	pool.Close()
	authentication := &issuerGateAuthentication{endpoint: &ResolvedMcpEndpoint{OrganizationID: "org_test"}}
	env, handled, err := service.builtinDirectAuthEnvironment(t.Context(), authentication, "slack")
	require.Error(t, err)
	require.True(t, handled)
	require.Nil(t, env.UserConfig)
	env, handled, err = service.builtinMemberAuthEnvironment(t.Context(), "org_test", metaMember{builtinKey: "slack"})
	require.Error(t, err)
	require.True(t, handled)
	require.Nil(t, env.UserConfig)
}

func TestBuiltinTypedMemberOnlyUnderEnforcement(t *testing.T) {
	t.Parallel()
	for _, enforced := range []bool{false, true} {
		choices := &memberChoices{enforced: enforced}
		require.Equal(t, enforced, choices.typedMember(metaMember{backend: metaMemberBackendBuiltin}))
		require.True(t, choices.typedMember(metaMember{backend: metaMemberBackendProxied}))
	}
}

func TestBuiltinMemberUsesSelectedStaticClient(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "builtin_auth_client")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.service.authEnforcement = authenforcement.NewGate(pool)
	f.enforce()
	clientID := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	f.copyHeader(clientID, "SLACK_BOT_TOKEN", true, false, text("test-secret"), pgtype.Text{})
	selector := f.choose(validUUID(clientID))
	choices := &memberChoices{selector: selector, enforced: true, subject: urn.NewUserSubject("user_test")}
	members := []metaMember{{backend: metaMemberBackendBuiltin, builtinKey: "slack", serverID: f.serverID, projectID: f.projectID}}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, ProjectID: &f.projectID})
	f.service.env = builtinEnvironmentFixture{load: func(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error) {
		t.Fatal("enforced gateway must never load environment credentials")
		return nil, nil
	}}
	// Eligibility runs first: absence of an environment or selection must not
	// hide the member before choices can resolve. Readiness is separate.
	supported, _, err := f.service.builtinMemberCapability(ctx, members[0])
	require.NoError(t, err)
	require.True(t, supported)
	ready, _, err := f.service.builtinMemberAuth(ctx, members[0])
	require.Error(t, err)
	require.False(t, ready)
	choices.apply(t.Context(), testenv.NewLogger(t), members, true)
	env, handled, err := f.service.builtinMemberAuthEnvironment(t.Context(), f.orgID, members[0])
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "test-secret", env.UserConfig.Get("SLACK_BOT_TOKEN"))
	ready, _, err = f.service.builtinMemberAuth(ctx, members[0])
	require.NoError(t, err)
	require.True(t, ready)

	f.service.authClientPermitter = denyPermitter{}
	// Permission denial must precede even attempting to decrypt this value.
	f.copyHeader(clientID, "SLACK_USER_TOKEN", true, true, text("invalid-ciphertext"), pgtype.Text{})
	members[0].authClient = nil
	choices.apply(t.Context(), testenv.NewLogger(t), members, true)
	require.Equal(t, authClientNoAccess, members[0].authClient.outcome)
	require.Nil(t, members[0].authClient.headers)
	ready, _, err = f.service.builtinMemberAuth(ctx, members[0])
	require.Error(t, err)
	require.False(t, ready)
	env, handled, err = f.service.builtinMemberAuthEnvironment(t.Context(), f.orgID, members[0])
	require.Error(t, err)
	require.True(t, handled)
	require.Nil(t, env.UserConfig)

	env, handled, err = f.service.builtinMemberAuthEnvironment(t.Context(), f.orgID, metaMember{builtinKey: "slack"})
	require.Error(t, err)
	require.True(t, handled)
	require.Nil(t, env.UserConfig)
	authentication := &issuerGateAuthentication{endpoint: &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}}
	env, handled, err = f.service.builtinDirectAuthEnvironment(t.Context(), authentication, "slack")
	require.Error(t, err)
	require.True(t, handled)
	require.Nil(t, env.UserConfig)
}

func TestBuiltinDirectUsesSelectedStaticClient(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "builtin_direct_static")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.service.siteURL = &url.URL{Scheme: "https", Host: "dashboard.example.com"}
	f.enforce()
	clientID := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	f.copyHeader(clientID, "SLACK_BOT_TOKEN", true, false, text("selected-token"), pgtype.Text{})
	keyID := uuid.New()
	_, err = upstreamclientsrepo.New(pool).RecordAuthClientChoice(t.Context(), upstreamclientsrepo.RecordAuthClientChoiceParams{CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keyID, Source: upstreamclients.SelectionSourceIssuance, OrganizationID: f.orgID, AuthClientID: validUUID(clientID), McpServerID: f.serverID})
	require.NoError(t, err)
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: keyID.String()}, agent, contextvalues.PrincipalCredential{AuthorizerUserID: "user_test"})
	authentication := &issuerGateAuthentication{endpoint: &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}, subject: urn.NewUserSubject("user_test")}
	f.service.env = builtinEnvironmentFixture{load: func(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error) {
		t.Fatal("selected direct credential must never read environment credentials")
		return nil, nil
	}}
	env, handled, err := f.service.builtinDirectAuthEnvironment(ctx, authentication, "slack")
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "selected-token", env.UserConfig.Get("SLACK_BOT_TOKEN"))
	require.Equal(t, clientID, authentication.authClient.clientID)
	f.service.authClientPermitter = denyPermitter{}
	env, handled, err = f.service.builtinDirectAuthEnvironment(ctx, authentication, "slack")
	require.Error(t, err)
	require.True(t, handled)
	require.Nil(t, env.UserConfig)
}

func TestAnonymousGatewayOmitsPrivateBuiltinCandidates(t *testing.T) {
	t.Parallel()
	// No database or auth engine is necessary: anonymous discovery must skip
	// private built-ins before any lookup, without blocking ordinary members.
	service := &Service{}
	members, err := service.storedBuiltinCandidates(t.Context(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.Empty(t, members)
	members, err = service.derivedBuiltinCandidates(t.Context(), "org_test", uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, members)
}
