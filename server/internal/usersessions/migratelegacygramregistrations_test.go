package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// migrateTestBaseURL must match the serverURL the test harness hands the
// remotesessions service, since the migration keys registrations by the
// default-domain MCP URL built from it.
const migrateTestBaseURL = "http://0.0.0.0"

func TestMigrateLegacyGramRegistrations_MigratesGramRegistrations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	providerID, serverID := seedProxyProvider(t, ctx, ti.conn, "gram-migrate", "gram")
	attachToolsetToProxyServer(t, ctx, ti.conn, serverID, "gram-migrate")
	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, "gram-migrate")

	mcpURL := migrateTestBaseURL + "/mcp/gram-migrate"
	seedLegacyRegistration(t, ctx, ti.redis, legacyRegistration(mcpURL, "gram_confidential", "gram-shhh", "Confidential", "client_secret_post"))
	seedLegacyRegistration(t, ctx, ti.redis, legacyRegistration(mcpURL, "gram_public", "", "Public", "none"))

	res, err := ti.service.MigrateLegacyGramRegistrations(ctx, &gen.MigrateLegacyGramRegistrationsPayload{
		OauthProxyProviderID: providerID.String(),
		UserSessionIssuerID:  issuerID.String(),
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, 2, res.MigratedCount)

	usq := repo.New(ti.conn)
	confidential, err := usq.GetUserSessionClientByClientID(ctx, repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: issuerID,
		ClientID:            "gram_confidential",
	})
	require.NoError(t, err, "gram registration becomes a user_session_clients row preserving client_id")
	require.Equal(t, "Confidential", confidential.ClientName)
	require.True(t, confidential.ClientSecretHash.Valid)

	public, err := usq.GetUserSessionClientByClientID(ctx, repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: issuerID,
		ClientID:            "gram_public",
	})
	require.NoError(t, err)
	require.False(t, public.ClientSecretHash.Valid, "public clients are PKCE-only: NULL hash")
}

func TestMigrateLegacyGramRegistrations_RejectsCustomProvider(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	providerID, _ := seedProxyProvider(t, ctx, ti.conn, "custom-reject", "custom")
	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, "custom-reject")

	_, err := ti.service.MigrateLegacyGramRegistrations(ctx, &gen.MigrateLegacyGramRegistrationsPayload{
		OauthProxyProviderID: providerID.String(),
		UserSessionIssuerID:  issuerID.String(),
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestMigrateLegacyGramRegistrations_IssuerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	providerID, _ := seedProxyProvider(t, ctx, ti.conn, "issuer-missing", "gram")

	_, err := ti.service.MigrateLegacyGramRegistrations(ctx, &gen.MigrateLegacyGramRegistrationsPayload{
		OauthProxyProviderID: providerID.String(),
		UserSessionIssuerID:  uuid.NewString(),
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestMigrateLegacyGramRegistrations_ProviderNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, "provider-missing")

	_, err := ti.service.MigrateLegacyGramRegistrations(ctx, &gen.MigrateLegacyGramRegistrationsPayload{
		OauthProxyProviderID: uuid.NewString(),
		UserSessionIssuerID:  issuerID.String(),
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestMigrateLegacyGramRegistrations_InvalidProviderID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, "bad-id")

	_, err := ti.service.MigrateLegacyGramRegistrations(ctx, &gen.MigrateLegacyGramRegistrationsPayload{
		OauthProxyProviderID: "not-a-uuid",
		UserSessionIssuerID:  issuerID.String(),
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// seedProxyProvider inserts an oauth_proxy_server + oauth_proxy_provider of the
// given type for the auth context's project. Returns the provider id and the
// server id.
func seedProxyProvider(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug, providerType string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	q := oauthrepo.New(conn)
	srv, err := q.UpsertOAuthProxyServer(ctx, oauthrepo.UpsertOAuthProxyServerParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "srv-" + slug,
		Audience:  conv.ToPGText("https://example.com"),
	})
	require.NoError(t, err)

	prov, err := q.UpsertOAuthProxyProvider(ctx, oauthrepo.UpsertOAuthProxyProviderParams{
		ProjectID:                         *authCtx.ProjectID,
		OauthProxyServerID:                srv.ID,
		Slug:                              slug,
		ProviderType:                      providerType,
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		RegistrationEndpoint:              conv.ToPGText("https://idp.example.com/register"),
		ScopesSupported:                   []string{"openid"},
		ResponseTypesSupported:            []string{"code"},
		ResponseModesSupported:            []string{"query"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		SecurityKeyNames:                  []string{},
		Secrets:                           []byte(`{}`),
	})
	require.NoError(t, err)
	return prov.ID, srv.ID
}

// attachToolsetToProxyServer seeds an MCP-enabled toolset routed through the
// given oauth_proxy_server under the given MCP slug.
func attachToolsetToProxyServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, proxyServerID uuid.UUID, mcpSlug string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	q := toolsetsrepo.New(conn)
	_, err := q.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   mcpSlug,
		Slug:                   mcpSlug,
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                conv.ToPGText(mcpSlug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	_, err = q.UpdateToolsetOAuthProxyServer(ctx, toolsetsrepo.UpdateToolsetOAuthProxyServerParams{
		OauthProxyServerID: uuid.NullUUID{UUID: proxyServerID, Valid: true},
		Slug:               mcpSlug,
		ProjectID:          *authCtx.ProjectID,
	})
	require.NoError(t, err)
}

func seedUserSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuer, err := repo.New(conn).CreateUserSessionIssuer(ctx, repo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func legacyRegistration(mcpURL, clientID, clientSecret, clientName, authMethod string) oauth.OauthProxyClientInfo {
	now := time.Now()
	return oauth.OauthProxyClientInfo{
		MCPURL:                  mcpURL,
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientSecretExpiresAt:   now.Add(15 * 24 * time.Hour),
		ClientName:              clientName,
		RedirectUris:            []string{"https://example.com/callback"},
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		Scope:                   "",
		TokenEndpointAuthMethod: authMethod,
		ApplicationType:         "web",
		CreatedAt:               now,
		UpdatedAt:               now,
	}
}

func seedLegacyRegistration(t *testing.T, ctx context.Context, r *redis.Client, info oauth.OauthProxyClientInfo) {
	t.Helper()
	typed := cache.NewTypedObjectCache[oauth.OauthProxyClientInfo](testenv.NewLogger(t), cache.NewRedisCacheAdapter(r), cache.SuffixNone)
	require.NoError(t, typed.Store(ctx, info))
}
