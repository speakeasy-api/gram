package toolsets_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func externalOAuthDiscoveryPolicy(t *testing.T, handler http.HandlerFunc) (string, *guardian.Policy, func()) {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(serverURL.Host)
	require.NoError(t, err)

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
	})
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	policy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		nil,
		guardian.WithResolver(resolver),
		guardian.WithTLSRootCAs(roots),
	)
	require.NoError(t, err)

	return "https://auth.example.com:" + port, policy, server.Close
}

func externalOAuthSlug(value string) *types.Slug {
	slug := types.Slug(value)
	return &slug
}

func gramHostedMetadata() map[string]any {
	return map[string]any{
		"issuer":                 "https://auth.example.com",
		"authorization_endpoint": "https://auth.example.com/authorize",
		"token_endpoint":         "https://auth.example.com/token",
		"registration_endpoint":  "https://auth.example.com/register",
	}
}

func attachMetadataOAuth(t *testing.T, ctx context.Context, ti *testInstance, slug types.Slug) *types.Toolset {
	t.Helper()

	result, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		Slug: slug, ExternalOauthServer: &types.ExternalOAuthServerForm{Metadata: gramHostedMetadata()},
	})
	require.NoError(t, err)
	return result
}

func TestToolsetsService_AddExternalOAuthServer_IssuerMode(t *testing.T) {
	t.Parallel()

	var issuer string
	issuer, policy, closeServer := externalOAuthDiscoveryPolicy(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token",
		})
	})
	defer closeServer()

	ctx, ti := newTestToolsetsService(t, policy)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Issuer Create Toolset")

	updated, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		Slug:                toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{AuthorizationServerIssuer: &issuer},
	})
	require.NoError(t, err)
	require.Nil(t, updated.ExternalOauthServer.Metadata)
	require.Equal(t, issuer, *updated.ExternalOauthServer.AuthorizationServerIssuer)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, true, metadata["authorization_server_issuer_set"])
}

func TestToolsetsService_UpdateExternalOAuthServer_SetAndClear(t *testing.T) {
	t.Parallel()

	var currentIssuer atomic.Pointer[string]
	var requests atomic.Int32
	issuer, policy, closeServer := externalOAuthDiscoveryPolicy(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		discoveredIssuer := currentIssuer.Load()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": *discoveredIssuer, "authorization_endpoint": *discoveredIssuer + "/authorize", "token_endpoint": *discoveredIssuer + "/token",
		})
	})
	storeIssuer := func(value string) { currentIssuer.Store(&value) }
	storeIssuer(issuer)

	ctx, ti := newTestToolsetsService(t, policy)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Issuer Update Toolset")
	created := attachMetadataOAuth(t, ctx, ti, toolset.Slug)
	externalOAuthID := created.ExternalOauthServer.ID

	set, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
		Slug: toolset.Slug, AuthorizationServerIssuer: &issuer,
	})
	require.NoError(t, err)
	require.Equal(t, externalOAuthID, set.ExternalOauthServer.ID)
	require.Nil(t, set.ExternalOauthServer.Metadata)
	require.Equal(t, issuer, *set.ExternalOauthServer.AuthorizationServerIssuer)
	require.Equal(t, int32(1), requests.Load())
	setAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetUpdateExternalOAuthIssuer)
	require.NoError(t, err)
	setAuditMetadata, err := audittest.DecodeAuditData(setAudit.Metadata)
	require.NoError(t, err)
	require.Equal(t, true, setAuditMetadata["authorization_server_issuer_set"])

	issuer += "/v2"
	storeIssuer(issuer)
	changed, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
		Slug: toolset.Slug, AuthorizationServerIssuer: &issuer,
	})
	require.NoError(t, err)
	require.Equal(t, externalOAuthID, changed.ExternalOauthServer.ID)
	require.Equal(t, issuer, *changed.ExternalOauthServer.AuthorizationServerIssuer)
	require.Equal(t, int32(2), requests.Load())

	closeServer()
	cleared, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
		Slug: toolset.Slug, Metadata: gramHostedMetadata(),
	})
	require.NoError(t, err)
	require.Equal(t, externalOAuthID, cleared.ExternalOauthServer.ID)
	require.Equal(t, gramHostedMetadata(), cleared.ExternalOauthServer.Metadata)
	require.Nil(t, cleared.ExternalOauthServer.AuthorizationServerIssuer)
	require.Equal(t, int32(2), requests.Load(), "clearing must not perform discovery")
	clearAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetUpdateExternalOAuthIssuer)
	require.NoError(t, err)
	clearAuditMetadata, err := audittest.DecodeAuditData(clearAudit.Metadata)
	require.NoError(t, err)
	require.Equal(t, false, clearAuditMetadata["authorization_server_issuer_set"])
}

func TestToolsetsService_AddExternalOAuthServer_FailedDiscoveryDoesNotPersist(t *testing.T) {
	t.Parallel()

	issuer, policy, closeServer := externalOAuthDiscoveryPolicy(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": "https://other.example.com", "authorization_endpoint": "https://other.example.com/authorize", "token_endpoint": "https://other.example.com/token",
		})
	})
	defer closeServer()

	ctx, ti := newTestToolsetsService(t, policy)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Atomic Issuer Create Toolset")

	_, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		Slug: toolset.Slug, ExternalOauthServer: &types.ExternalOAuthServerForm{AuthorizationServerIssuer: &issuer},
	})
	require.ErrorContains(t, err, "invalid authorization server issuer")

	unchanged, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{Slug: toolset.Slug})
	require.NoError(t, err)
	require.Nil(t, unchanged.ExternalOauthServer)
}

func TestToolsetsService_UpdateExternalOAuthServer_DiscoversBeforeTakingToolsetLock(t *testing.T) {
	t.Parallel()

	requested := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseDiscovery := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseDiscovery)

	var issuer string
	var policy *guardian.Policy
	var closeServer func()
	issuer, policy, closeServer = externalOAuthDiscoveryPolicy(t, func(w http.ResponseWriter, _ *http.Request) {
		close(requested)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token",
		})
	})
	defer closeServer()

	ctx, ti := newTestToolsetsService(t, policy)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Discovery Before Lock Toolset")
	attachMetadataOAuth(t, ctx, ti, toolset.Slug)

	result := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
			Slug: toolset.Slug, AuthorizationServerIssuer: &issuer,
		})
		result <- err
	}()
	<-requested

	probe := testenv.BeginTx(t, ctx, ti.conn)
	_, lockErr := probe.Exec(ctx, `SELECT id FROM toolsets WHERE project_id = $1 AND slug = $2 FOR UPDATE NOWAIT`, toolset.ProjectID, toolset.Slug) //nolint:glint // notestingrawsql: NOWAIT proves discovery does not hold the toolset lock
	_ = probe.Rollback(ctx)
	releaseDiscovery()
	require.NoError(t, <-result)
	require.NoError(t, lockErr)
}

func TestToolsetsService_UpdateExternalOAuthServer_FailedDiscoveryIsAtomic(t *testing.T) {
	t.Parallel()

	issuer, policy, closeServer := externalOAuthDiscoveryPolicy(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": "https://other.example.com", "authorization_endpoint": "https://other.example.com/authorize", "token_endpoint": "https://other.example.com/token",
		})
	})
	defer closeServer()

	ctx, ti := newTestToolsetsService(t, policy)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Atomic Issuer Toolset")
	created := attachMetadataOAuth(t, ctx, ti, toolset.Slug)

	_, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
		Slug: toolset.Slug, AuthorizationServerIssuer: &issuer,
	})
	require.ErrorContains(t, err, "invalid authorization server issuer")

	unchanged, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{Slug: toolset.Slug})
	require.NoError(t, err)
	require.Equal(t, created.ExternalOauthServer, unchanged.ExternalOauthServer)
}

func TestToolsetsService_ExternalOAuthServer_RequiresExactlyOneSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Issuer XOR Toolset")

	for _, form := range []*types.ExternalOAuthServerForm{
		{},
		{Metadata: gramHostedMetadata(), AuthorizationServerIssuer: new(string)},
		{Metadata: []any{"not-an-object"}},
	} {
		_, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{Slug: toolset.Slug, ExternalOauthServer: form})
		require.Error(t, err)
	}

	created := attachMetadataOAuth(t, ctx, ti, toolset.Slug)

	for _, payload := range []*gen.UpdateExternalOAuthServerPayload{
		{Slug: toolset.Slug},
		{Slug: toolset.Slug, Metadata: gramHostedMetadata(), AuthorizationServerIssuer: new(string)},
	} {
		_, err := ti.service.UpdateExternalOAuthServer(ctx, payload)
		require.Error(t, err)
	}

	unchanged, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{Slug: toolset.Slug})
	require.NoError(t, err)
	require.Equal(t, created.ExternalOauthServer, unchanged.ExternalOauthServer)
}

func TestToolsetsService_UpdateExternalOAuthServer_RequiresAttachedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Missing External OAuth Toolset")

	_, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
		Slug: toolset.Slug, Metadata: gramHostedMetadata(),
	})
	require.ErrorContains(t, err, "external OAuth server is not attached")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestToolsetsService_UpdateExternalOAuthServer_CannotAccessAnotherProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Issuer Project Scope Toolset")
	attachMetadataOAuth(t, ctx, ti, toolset.Slug)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherProjectID := uuid.New()
	authCtx.ProjectID = &otherProjectID
	otherProjectCtx := contextvalues.SetAuthContext(ctx, authCtx)
	_, err := ti.service.UpdateExternalOAuthServer(otherProjectCtx, &gen.UpdateExternalOAuthServerPayload{
		Slug: toolset.Slug, Metadata: gramHostedMetadata(),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestToolsetsService_UpdateExternalOAuthServer_HonorsProjectScopedAuthorization(t *testing.T) {
	t.Parallel()

	projectGrant := func(projectID string, scope authz.Scope) authz.Grant {
		return authz.NewGrantWithSelector(scope, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID,
		})
	}

	for _, tc := range []struct {
		name    string
		grants  func(string) []authz.Grant
		allowed bool
	}{
		{name: "no grants", grants: func(string) []authz.Grant { return nil }},
		{name: "matching project grant", grants: func(projectID string) []authz.Grant {
			return []authz.Grant{projectGrant(projectID, authz.ScopeMCPWrite)}
		}, allowed: true},
		{name: "other project grant", grants: func(string) []authz.Grant {
			return []authz.Grant{projectGrant(uuid.NewString(), authz.ScopeMCPWrite)}
		}},
		{name: "matching project exclusion", grants: func(projectID string) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
				projectGrant(projectID, authz.ScopeMCPBlockedWrite),
			}
		}},
		{name: "other project exclusion", grants: func(string) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
				projectGrant(uuid.NewString(), authz.ScopeMCPBlockedWrite),
			}
		}, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestToolsetsService(t)
			ctx = withAccountType(t, ctx, "pro")
			toolset := createMinimalPublicToolset(t, ctx, ti, "Issuer Project Authorization Toolset")
			attachMetadataOAuth(t, ctx, ti, toolset.Slug)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			ctx = authztest.WithExactGrants(t, ctx, tc.grants(authCtx.ProjectID.String())...)
			_, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
				Slug: toolset.Slug, Metadata: gramHostedMetadata(),
			})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeForbidden, oopsErr.Code)
			}
		})
	}
}

func TestToolsetsService_UpdateExternalOAuthServer_AuditIsSecretFree(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Issuer Audit Toolset")
	created := attachMetadataOAuth(t, ctx, ti, toolset.Slug)

	updated, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
		Slug: toolset.Slug, Metadata: map[string]any{"issuer": "https://auth.example.com", "token_endpoint": "secret-token-value"},
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetUpdateExternalOAuthIssuer)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, created.ExternalOauthServer.ID, metadata["external_oauth_server_id"])
	require.Equal(t, string(created.ExternalOauthServer.Slug), metadata["external_oauth_server_slug"])
	require.Equal(t, false, metadata["authorization_server_issuer_set"])
	require.InDelta(t, updated.ToolsetVersion, metadata["toolset_version_after"], 0)
	require.NotContains(t, string(record.Metadata), "secret-token-value")
}
