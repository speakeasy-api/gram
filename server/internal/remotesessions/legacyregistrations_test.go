package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// legacyRegistrationFixture returns an oauth.OauthProxyClientInfo the way the
// legacy DCR endpoint would have stored it for the given MCP URL.
func legacyRegistrationFixture(mcpURL, clientID, clientSecret, clientName, authMethod string, redirectURIs []string) oauth.OauthProxyClientInfo {
	now := time.Now()
	return oauth.OauthProxyClientInfo{
		MCPURL:                  mcpURL,
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientSecretExpiresAt:   now.Add(15 * 24 * time.Hour),
		ClientName:              clientName,
		RedirectUris:            redirectURIs,
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		Scope:                   "",
		TokenEndpointAuthMethod: authMethod,
		ApplicationType:         "web",
		CreatedAt:               now,
		UpdatedAt:               now,
	}
}

func TestCloneClientFromOAuthProxyProvider_MigratesLegacyRegistrations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID, proxyServerID := insertProxyProvider(t, ctx, ti.conn, "clone-migrate", "custom", secrets)
	attachToolsetToProxyServer(t, ctx, ti.conn, proxyServerID, "clone-migrate", "")
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-migrate", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-migrate")

	mcpURL := testServerURL + "/mcp/clone-migrate"
	seedLegacyRegistration(t, ctx, ti, legacyRegistrationFixture(
		mcpURL, "client_confidential", "legacy-shhh", "Confidential MCP Client",
		"client_secret_post", []string{"https://claude.ai/api/mcp/auth_callback"},
	))
	seedLegacyRegistration(t, ctx, ti, legacyRegistrationFixture(
		mcpURL, "client_public", "", "Public MCP Client",
		"none", []string{"http://127.0.0.1:33418/callback"},
	))

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID.String()},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	usq := usersessionsrepo.New(ti.conn)

	confidential, err := usq.GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: userIssuerID,
		ClientID:            "client_confidential",
	})
	require.NoError(t, err, "confidential registration becomes a user_session_clients row preserving client_id")
	require.Equal(t, "Confidential MCP Client", confidential.ClientName)
	require.Equal(t, []string{"https://claude.ai/api/mcp/auth_callback"}, confidential.RedirectUris)
	require.True(t, confidential.ClientSecretHash.Valid, "confidential clients keep a usable secret")
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(confidential.ClientSecretHash.String), []byte("legacy-shhh")), "hash verifies against the original plaintext secret")
	require.False(t, confidential.ClientSecretExpiresAt.Valid, "migrated secrets do not inherit the legacy 15-day expiry")

	public, err := usq.GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: userIssuerID,
		ClientID:            "client_public",
	})
	require.NoError(t, err)
	require.False(t, public.ClientSecretHash.Valid, "public (token_endpoint_auth_method=none) clients are PKCE-only: NULL hash")
}

func TestCloneClientFromOAuthProxyProvider_MigratesCustomDomainRegistrations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID, proxyServerID := insertProxyProvider(t, ctx, ti.conn, "clone-migrate-cd", "custom", secrets)
	attachToolsetToProxyServer(t, ctx, ti.conn, proxyServerID, "clone-migrate-cd", "mcp.acme.example.com")
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-migrate-cd", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-migrate-cd")

	// Registrations are keyed by whichever URL the MCP client hit; this one
	// came in through the custom domain, not the default domain.
	seedLegacyRegistration(t, ctx, ti, legacyRegistrationFixture(
		"https://mcp.acme.example.com/mcp/clone-migrate-cd", "client_custom_domain", "cd-shhh",
		"Custom Domain MCP Client", "client_secret_basic", []string{"https://example.com/callback"},
	))

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID.String()},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	migrated, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: userIssuerID,
		ClientID:            "client_custom_domain",
	})
	require.NoError(t, err, "registrations keyed under the custom-domain URL variant are found and migrated")
	require.Equal(t, "Custom Domain MCP Client", migrated.ClientName)
}

func TestCloneClientFromOAuthProxyProvider_RegistrationMigrationIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID, proxyServerID := insertProxyProvider(t, ctx, ti.conn, "clone-migrate-dup", "custom", secrets)
	attachToolsetToProxyServer(t, ctx, ti.conn, proxyServerID, "clone-migrate-dup", "")
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-migrate-dup", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-migrate-dup")

	mcpURL := testServerURL + "/mcp/clone-migrate-dup"
	seedLegacyRegistration(t, ctx, ti, legacyRegistrationFixture(
		mcpURL, "client_dup", "redis-shhh", "From Redis",
		"client_secret_post", []string{"https://example.com/redis-callback"},
	))

	// An active row for the same client_id already exists; the migration must
	// neither duplicate nor clobber it.
	usq := usersessionsrepo.New(ti.conn)
	_, err := usq.CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID:   userIssuerID,
		ClientID:              "client_dup",
		ClientSecretHash:      pgtype.Text{},
		ClientName:            "Pre-existing",
		RedirectUris:          []string{"https://example.com/original-callback"},
		ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	_, err = ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID.String()},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	row, err := usq.GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: userIssuerID,
		ClientID:            "client_dup",
	})
	require.NoError(t, err)
	require.Equal(t, "Pre-existing", row.ClientName, "existing active row is not clobbered")
	require.Equal(t, []string{"https://example.com/original-callback"}, row.RedirectUris)

	rows, err := usq.ListUserSessionClientsByProjectID(ctx, usersessionsrepo.ListUserSessionClientsByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
		Cursor:              uuid.NullUUID{},
		LimitValue:          10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "no duplicate rows after repeated clones")
}

func TestCloneClientFromOAuthProxyProvider_RegistrationMigrationFailureAbortsClone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID, proxyServerID := insertProxyProvider(t, ctx, ti.conn, "clone-migrate-bad", "custom", secrets)
	attachToolsetToProxyServer(t, ctx, ti.conn, proxyServerID, "clone-migrate-bad", "")
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-migrate-bad", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-migrate-bad")

	mcpURL := testServerURL + "/mcp/clone-migrate-bad"
	seedLegacyRegistration(t, ctx, ti, legacyRegistrationFixture(
		mcpURL, "client_good", "good-shhh", "Good MCP Client",
		"client_secret_post", []string{"https://example.com/callback"},
	))
	// A registration entry that cannot be decoded; reading it must fail the
	// whole clone rather than commit a partial migration.
	badKey := "oauthClientInfo:" + mcpURL + ":client_bad:"
	require.NoError(t, ti.redisCache.Set(ctx, badKey, "not a registration", time.Hour))

	payload := &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID.String()},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	}
	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, payload)
	require.Error(t, err)

	// The transaction rolled back: no cloned remote_session_client and no
	// migrated user_session_clients rows, including the readable one.
	issuerUUID, err := uuid.Parse(issuerID)
	require.NoError(t, err)
	clientCount, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, issuerUUID)
	require.NoError(t, err)
	require.Equal(t, int64(0), clientCount)

	_, err = usersessionsrepo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: userIssuerID,
		ClientID:            "client_good",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// The operator workflow: fix the cause and re-run the clone. Nothing from
	// the failed attempt blocks the retry, which now migrates everything.
	require.NoError(t, ti.redisCache.Delete(ctx, badKey))

	_, err = ti.service.CloneClientFromOAuthProxyProvider(ctx, payload)
	require.NoError(t, err, "re-running after fixing the cause succeeds")

	migrated, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: userIssuerID,
		ClientID:            "client_good",
	})
	require.NoError(t, err)
	require.Equal(t, "Good MCP Client", migrated.ClientName)
}
