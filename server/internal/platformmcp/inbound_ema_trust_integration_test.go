package platformmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	orgremotegen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
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
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type inboundEMAServicesFixture struct {
	conn      *pgxpool.Pool
	project   ResolvedProject
	remote    *remotesessions.Service
	principal Principal
	service   *InboundEMATrustService
	issuerID  uuid.UUID
	idpURL    string
}

// newInboundEMAServicesFixture wires the tools to the real organization issuer
// management services. maxConns limits the pool when positive.
func newInboundEMAServicesFixture(t *testing.T, maxConns int32) (context.Context, inboundEMAServicesFixture) {
	t.Helper()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_inbound_ema_services")
	require.NoError(t, err)
	if maxConns > 0 {
		config := conn.Config()
		config.MaxConns = maxConns
		conn, err = pgxpool.NewWithConfig(ctx, config)
		require.NoError(t, err)
		t.Cleanup(conn.Close)
	}
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
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
	return ctx, inboundEMAServicesFixture{conn: conn, project: project, remote: remote, principal: principal, service: service, issuerID: issuer.ID, idpURL: idp.URL}
}

// The tools act through the real organization issuer management services, so
// their validation and audit apply to every trust change.
func TestInboundEMATrustThroughManagementServices(t *testing.T) {
	t.Parallel()
	ctx, f := newInboundEMAServicesFixture(t, 0)
	conn, principal, service := f.conn, f.principal, f.service

	preview, err := service.Trust(ctx, principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID.String(), IssuerURL: f.idpURL})
	require.NoError(t, err)
	require.False(t, preview.Applied)
	require.Empty(t, preview.Warnings)

	trusted, err := service.Trust(ctx, principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID.String(), IssuerURL: f.idpURL, ExpectedTrustedRemoteSessionIssuerID: preview.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	require.NoError(t, err)
	require.True(t, trusted.Applied)
	require.False(t, trusted.IdentityProviderReused)
	require.Equal(t, f.idpURL+"/keys", trusted.IdentityProvider.KeySetURL)
	require.NotNil(t, trusted.Issuer.TrustedIdentityProvider)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, trusted.Issuer.TrustedIdentityProvider.RemoteSessionIssuerID)
	require.Equal(t, "https://app.example.test/oauth/usi/"+f.issuerID.String(), trusted.Issuer.Audience)

	created, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, created.SubjectID)
	updated, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionUserSessionIssuerUpdate, f.issuerID.String())
	require.NoError(t, err)
	require.Equal(t, principal.UserID, updated.ActorID)
	after, err := audittest.DecodeAuditData(updated.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, after["TrustedRemoteSessionIssuerID"])

	untrustPreview, err := service.Untrust(ctx, principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID.String()})
	require.NoError(t, err)
	require.Equal(t, trusted.IdentityProvider.RemoteSessionIssuerID, untrustPreview.ExpectedTrustedRemoteSessionIssuerID)
	untrusted, err := service.Untrust(ctx, principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID.String(), ExpectedTrustedRemoteSessionIssuerID: untrustPreview.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	require.NoError(t, err)
	require.True(t, untrusted.Applied)
	require.Nil(t, untrusted.Issuer.TrustedIdentityProvider)

	cleared, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionUserSessionIssuerUpdate, f.issuerID.String())
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

// Concurrent confirmations on a small pool all finish: a caller that cannot
// take the provider lock is refused at once instead of holding a connection
// the lock holder's management calls need.
func TestInboundEMATrustConcurrentConfirmationsOnSmallPool(t *testing.T) {
	t.Parallel()
	ctx, f := newInboundEMAServicesFixture(t, 3)
	ctx, cancel := context.WithTimeout(ctx, inboundEMASmallPoolDeadline)
	defer cancel()
	input := TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID.String(), IssuerURL: f.idpURL, Confirmed: true}

	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = f.service.Trust(ctx, f.principal, input)
		})
	}
	wg.Wait()
	require.NoError(t, ctx.Err(), "every confirmation finishes before the deadline")
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		var refusal *InboundEMATrustRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, inboundEMARefusalConflict, refusal.Code)
	}
	require.Positive(t, succeeded)

	records, err := remotesessionsrepo.New(f.conn).ListOrganizationRemoteSessionIssuersByIssuerURL(ctx, remotesessionsrepo.ListOrganizationRemoteSessionIssuersByIssuerURLParams{
		Issuers: []string{f.idpURL}, OrganizationID: conv.ToPGText(f.principal.OrganizationID), IncludeGlobal: false, PerTierLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, records, 1, "exactly one identity provider record is created")
}

// inboundEMASmallPoolDeadline bounds the small-pool test; a starved pool
// would hang until it.
const inboundEMASmallPoolDeadline = 30 * time.Second

// An inbound-only link is not read by outbound chaining, so an issuer with
// active EMA bindings can still gain and lose one.
func TestInboundEMATrustIgnoresEMABindingsForInboundOnlyLink(t *testing.T) {
	t.Parallel()
	ctx, f := newInboundEMAServicesFixture(t, 0)
	vendor, err := f.remote.CreateIssuer(ctx, &orgremotegen.CreateIssuerPayload{Slug: "vendor-" + uuid.NewString()[:8], Issuer: "https://vendor.example.test"})
	require.NoError(t, err)
	_, err = testrepo.New(f.conn).InsertRemoteSessionEMABindingFixture(ctx, testrepo.InsertRemoteSessionEMABindingFixtureParams{
		ProjectID:             f.project.ID,
		OrganizationID:        f.principal.OrganizationID,
		UserSessionIssuerID:   f.issuerID,
		RemoteSessionIssuerID: uuid.MustParse(vendor.ID),
		Resource:              "https://vendor.example.test/mcp",
		RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	preview, err := f.service.Trust(ctx, f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID.String(), IssuerURL: f.idpURL})
	require.NoError(t, err)
	require.True(t, preview.Changed)
	trusted, err := f.service.Trust(ctx, f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID.String(), IssuerURL: f.idpURL, ExpectedTrustedRemoteSessionIssuerID: preview.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	require.NoError(t, err)
	require.True(t, trusted.Applied)
	require.NotNil(t, trusted.Issuer.TrustedIdentityProvider)

	untrusted, err := f.service.Untrust(ctx, f.principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID.String(), ExpectedTrustedRemoteSessionIssuerID: trusted.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	require.NoError(t, err)
	require.True(t, untrusted.Applied)
	require.Nil(t, untrusted.Issuer.TrustedIdentityProvider)

	bindings, err := remotesessionsrepo.New(f.conn).CountActiveEMABindingsForUserIssuer(ctx, remotesessionsrepo.CountActiveEMABindingsForUserIssuerParams{IssuerID: f.issuerID, OrganizationID: f.principal.OrganizationID, ProjectID: uuid.Nil})
	require.NoError(t, err)
	require.EqualValues(t, 1, bindings, "the binding is left as it was")
}
