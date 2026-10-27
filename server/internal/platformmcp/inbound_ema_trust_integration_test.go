package platformmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The tools act through the real organization issuer management services, so
// their validation and audit apply to every trust change.
func TestInboundEMATrustThroughManagementServices(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_inbound_ema_services")
	require.NoError(t, err)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	logger, tracer, meter := testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t)

	var metadata atomic.Pointer[[]byte]
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, writeErr := w.Write(*metadata.Load()); writeErr != nil {
			t.Errorf("write metadata: %v", writeErr)
		}
	}))
	t.Cleanup(idp.Close)
	document, err := json.Marshal(map[string]any{
		"issuer":                   idp.URL,
		"authorization_endpoint":   idp.URL + "/authorize",
		"token_endpoint":           idp.URL + "/token",
		"jwks_uri":                 idp.URL + "/keys",
		"response_types_supported": []string{"code"},
	})
	require.NoError(t, err)
	metadata.Store(&document)

	policy, err := guardian.NewUnsafePolicy(tracer, []string{})
	require.NoError(t, err)
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	enc := testenv.NewEncryptionClient(t)
	serverURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	hosts := authserver.Hosts{ServerURL: serverURL.String(), AuthenticationHostBaseURL: "", PlatformHosts: nil}
	users := usersessions.NewService(logger, tracer, meter, conn, &sessions.Manager{}, nil, engine, audit.NewLogger(), policy, nil, enc, usersessions.NewSigner("test-jwt-secret"), serverURL.String(), hosts, ratelimit.NewRedisStore(redisClient), func(_ context.Context, _ string) bool { return false })
	remote := remotesessions.NewService(logger, tracer, meter, conn, &sessions.Manager{}, engine, enc,
		environments.NewEnvironmentEntries(logger, conn, enc, mcpmetadatarepo.New(conn)), policy, nil, audit.NewLogger(), serverURL,
		remotesessions.NewIdentityCommitter(logger, conn, enc, audit.NewLogger(), serverURL, policy, nil, nil),
		remotesessions.NewRefreshService(logger, meter, conn, enc, policy, nil, cache.NewRedisCacheAdapter(redisClient)),
		productfeatures.NewClient(logger, tracer, conn, redisClient), nil)

	issuer, err := usersessionsrepo.New(conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(principal.OrganizationID),
		Slug:                         "workforce-" + uuid.NewString()[:8],
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TrustedRemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	_, err = testrepo.New(conn).SetUserSessionIssuerAuthorizationServerModeFixture(ctx, testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{
		AuthorizationServerMode: string(authserver.ModeShared), PinnedIssuerUrl: pgtype.Text{}, IssuerID: issuer.ID, OrganizationID: principal.OrganizationID,
	})
	require.NoError(t, err)

	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = contextWithPrincipal(ctx, principal)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID), authz.NewGrant(authz.ScopeOrgRead, principal.OrganizationID)})
	service := NewPostgresReader(logger, conn).WithInboundEMATrust(users, remote).inboundEMATrust

	preview, err := service.Trust(ctx, principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: issuer.ID.String(), IssuerURL: idp.URL})
	require.NoError(t, err)
	require.False(t, preview.Applied)
	require.Empty(t, preview.Warnings)

	trusted, err := service.Trust(ctx, principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: issuer.ID.String(), IssuerURL: idp.URL, ExpectedTrustedRemoteSessionIssuerID: preview.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	require.NoError(t, err)
	require.True(t, trusted.Applied)
	require.False(t, trusted.IdentityProviderReused)
	require.Equal(t, idp.URL+"/keys", trusted.IdentityProvider.KeySetURL)
	require.NotNil(t, trusted.Issuer.TrustedIdentityProvider)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, trusted.Issuer.TrustedIdentityProvider.RemoteSessionIssuerID)
	require.Equal(t, "https://app.example.test/oauth/usi/"+issuer.ID.String(), trusted.Issuer.Audience)

	created, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, created.SubjectID)
	updated, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionUserSessionIssuerUpdate, issuer.ID.String())
	require.NoError(t, err)
	require.Equal(t, principal.UserID, updated.ActorID)
	after, err := audittest.DecodeAuditData(updated.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, after["TrustedRemoteSessionIssuerID"])

	untrustPreview, err := service.Untrust(ctx, principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: issuer.ID.String()})
	require.NoError(t, err)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, untrustPreview.ExpectedTrustedRemoteSessionIssuerID)
	untrusted, err := service.Untrust(ctx, principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: issuer.ID.String(), ExpectedTrustedRemoteSessionIssuerID: untrustPreview.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	require.NoError(t, err)
	require.True(t, untrusted.Applied)
	require.Nil(t, untrusted.Issuer.TrustedIdentityProvider)

	cleared, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionUserSessionIssuerUpdate, issuer.ID.String())
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(cleared.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, before["TrustedRemoteSessionIssuerID"])
	after, err = audittest.DecodeAuditData(cleared.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, after["TrustedRemoteSessionIssuerID"])
	updates, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 2, updates)
}
