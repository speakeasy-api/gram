package remotesessionprovider_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/remotesessionprovider"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var verticalSliceInfra *testenv.Environment

func TestMain(m *testing.M) {
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	verticalSliceInfra = infra

	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func TestReviewedRemoteSessionProviderVerticalSlice(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := verticalSliceInfra.CloneTestDatabase(t, "platform_mcp_reviewed_remote_session")
	require.NoError(t, err)

	upstream := newReviewedUpstream(t)
	principal, project := seedPlatformRegistration(t, ctx, conn)
	store, err := platformmcp.NewRegistrationStore(conn, platformmcp.RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)
	registration := registerReviewedMCP(t, ctx, conn, store, principal, project, upstream.URL+"/mcp")

	probePolicy := testProbeFixturePolicy(t, upstream)
	manager := newChallengeManager(t, conn, testOAuthFixturePolicy(t, upstream))
	remoteIssuerID := seedReviewedRemoteIssuer(t, ctx, conn, project.ID, upstream.URL)
	adapter := remotesessionprovider.New(probePolicy, manager, remotesessionprovider.Descriptor{
		ProviderKey:                "fixture",
		RemoteSessionIssuerID:      remoteIssuerID,
		StreamableHTTPURL:          upstream.URL + "/mcp",
		ProviderSetupCompletionURL: "https://gram.test/platform-mcp/provider-setup-complete",
		TestOnlyAllowedCIDRBlocks:  []string{fixtureCIDR(t, upstream.URL)},
	})
	adapters := platformmcp.NewProviderAdapters([]platformmcp.ProviderAdapter{adapter})

	readiness, err := store.ProbeProviderReadiness(ctx, principal, project.ID, registration.ID, adapters)
	require.NoError(t, err)
	require.Equal(t, platformmcp.ReadinessNeedsConfiguration, readiness.State)

	remoteClientID := seedReviewedRemoteClient(t, ctx, conn, project.ID, principal.OrganizationID, registration.UserSessionIssuerID.UUID, remoteIssuerID)
	readiness, err = store.ProbeProviderReadiness(ctx, principal, project.ID, registration.ID, adapters)
	require.NoError(t, err)
	require.Equal(t, platformmcp.ReadinessNeedsGramAuthorization, readiness.State)

	handoff, err := store.IssueSetupHandoff(ctx, principal, platformmcp.SetupHandoffBinding{
		ProjectID:        project.ID,
		RegistrationID:   registration.ID,
		ProviderKey:      "fixture",
		CatalogReference: "reviewed-fixture",
		Intent:           "provider_setup",
	}, time.Now().UTC())
	require.NoError(t, err)

	setup, err := store.BeginProviderSetup(ctx, principal, platformmcp.SetupHandoffBinding{
		ProjectID:        project.ID,
		RegistrationID:   registration.ID,
		ProviderKey:      "fixture",
		CatalogReference: "reviewed-fixture",
		Intent:           "provider_setup",
	}, handoff.Value, adapters)
	require.NoError(t, err)
	require.NotEmpty(t, setup.AuthorizationURL)

	callbackURL := visitProviderAuthorization(t, upstream, setup.AuthorizationURL)
	callback := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(http.MethodGet, callbackURL.RequestURI(), nil)
	require.NoError(t, manager.HandleRemoteLoginCallback(callback, callbackRequest))
	require.Equal(t, http.StatusSeeOther, callback.Code)
	require.Equal(t, "https://gram.test/platform-mcp/provider-setup-complete", callback.Header().Get("Location"))

	remoteSession, err := remotesessionsrepo.New(conn).GetActiveRemoteSession(ctx, remotesessionsrepo.GetActiveRemoteSessionParams{
		SubjectUrn:            urn.NewUserSubject(principal.UserID),
		RemoteSessionClientID: remoteClientID,
	})
	require.NoError(t, err)

	readiness, err = store.ProbeProviderReadiness(ctx, principal, project.ID, registration.ID, adapters)
	require.NoError(t, err)
	require.Equal(t, platformmcp.ReadinessReady, readiness.State)
	require.Equal(t, "tools_list_ok", readiness.EvidenceCode)
	require.Zero(t, upstream.standaloneSSERequests.Load(), "readiness probes must not open standalone SSE streams")

	fingerprint, err := testrepo.New(conn).GetPlatformMCPReadinessFingerprintFixture(ctx, registration.ID)
	require.NoError(t, err)
	require.NotEmpty(t, fingerprint)
	require.NotContains(t, fingerprint, remoteSession.AccessTokenEncrypted)

	require.NoError(t, testrepo.New(conn).ExpireRemoteSessionAccessTokenFixture(ctx, remoteSession.ID))
	refreshed, err := store.ProbeProviderReadiness(ctx, principal, project.ID, registration.ID, adapters)
	require.NoError(t, err)
	require.Equal(t, platformmcp.ReadinessReady, refreshed.State)
	refreshedFingerprint, err := testrepo.New(conn).GetPlatformMCPReadinessFingerprintFixture(ctx, registration.ID)
	require.NoError(t, err)
	require.NotEqual(t, fingerprint, refreshedFingerprint, "refresh must replace persisted readiness evidence")

	upstream.rejectProbeAuthorization.Store(true)
	unauthorized, err := store.ProbeProviderReadiness(ctx, principal, project.ID, registration.ID, adapters)
	require.NoError(t, err)
	require.Equal(t, platformmcp.ReadinessUnauthorized, unauthorized.State)
	upstream.rejectProbeAuthorization.Store(false)

	upstream.mode.Store(upstreamModeRedirect)
	redirected, err := adapter.ProbeReadiness(ctx, providerProbeRequest(principal, project.ID, registration))
	require.NoError(t, err)
	require.Equal(t, platformmcp.ReadinessUnsupported, redirected.State)
	require.Equal(t, "redirect_rejected", redirected.EvidenceCode)

	upstream.mode.Store(upstreamModeOverflow)
	overflow, err := adapter.ProbeReadiness(ctx, providerProbeRequest(principal, project.ID, registration))
	require.NoError(t, err)
	require.Equal(t, platformmcp.ReadinessUnsupported, overflow.State)
	require.Equal(t, "response_too_large", overflow.EvidenceCode)
	upstream.mode.Store(upstreamModeNormal)

	_, err = remotesessionsrepo.New(conn).RevokeRemoteSession(ctx, remotesessionsrepo.RevokeRemoteSessionParams{ID: remoteSession.ID, ProjectID: project.ID})
	require.NoError(t, err)
	revoked, err := store.ProbeProviderReadiness(ctx, principal, project.ID, registration.ID, adapters)
	require.NoError(t, err)
	require.Equal(t, platformmcp.ReadinessNeedsGramAuthorization, revoked.State)
}

type reviewedUpstream struct {
	*httptest.Server
	rejectProbeAuthorization atomic.Bool
	standaloneSSERequests    atomic.Int64
	mode                     atomic.Int32
}

const (
	upstreamModeNormal int32 = iota
	upstreamModeRedirect
	upstreamModeOverflow
)

func newReviewedUpstream(t *testing.T) *reviewedUpstream {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "reviewed-fixture", Version: "1.0.0"}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	upstream := &reviewedUpstream{}
	upstream.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			redirectURI, err := url.Parse(r.URL.Query().Get("redirect_uri"))
			if err != nil {
				t.Errorf("parse reviewed provider redirect_uri: %v", err)
				http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
				return
			}
			query := redirectURI.Query()
			query.Set("code", "reviewed-provider-code")
			query.Set("state", r.URL.Query().Get("state"))
			redirectURI.RawQuery = query.Encode()
			http.Redirect(w, r, redirectURI.String(), http.StatusFound)
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse reviewed provider token request: %v", err)
				http.Error(w, "invalid token request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if r.Form.Get("grant_type") == "refresh_token" {
				_, _ = w.Write([]byte(`{"access_token":"reviewed-provider-refreshed-token","refresh_token":"reviewed-provider-refreshed-refresh","token_type":"Bearer","expires_in":3600}`))
				return
			}
			if r.Form.Get("grant_type") != "authorization_code" {
				t.Errorf("unexpected reviewed provider grant_type: %q", r.Form.Get("grant_type"))
				http.Error(w, "unsupported grant_type", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"reviewed-provider-access-token","refresh_token":"reviewed-provider-refresh-token","token_type":"Bearer","expires_in":3600}`))
		case "/mcp":
			if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
				upstream.standaloneSSERequests.Add(1)
			}
			if upstream.rejectProbeAuthorization.Load() || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer reviewed-provider-") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			switch upstream.mode.Load() {
			case upstreamModeRedirect:
				http.Redirect(w, r, "/mcp", http.StatusFound)
				return
			case upstreamModeOverflow:
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Length", strconv.Itoa(1<<20+1))
				_, _ = w.Write(bytes.Repeat([]byte("x"), 1<<20+1))
				return
			}
			mcpHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func testProbeFixturePolicy(t *testing.T, upstream *reviewedUpstream) *guardian.Policy {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	return guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithTLSRootCAs(roots))
}

func testOAuthFixturePolicy(t *testing.T, upstream *reviewedUpstream) *guardian.Policy {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	return policy
}

func fixtureCIDR(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	host, _, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	address := net.ParseIP(host)
	require.NotNil(t, address)
	return address.String() + "/32"
}

func newChallengeManager(t *testing.T, conn *pgxpool.Pool, policy *guardian.Policy) *remotesessions.ChallengeManager {
	t.Helper()
	redisClient, err := verticalSliceInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	baseURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)
	return remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, testenv.NewEncryptionClient(t), policy, cache.NewRedisCacheAdapter(redisClient), baseURL)
}

func seedPlatformRegistration(t *testing.T, ctx context.Context, conn *pgxpool.Pool) (platformmcp.Principal, platformmcp.ResolvedProject) {
	t.Helper()
	organizationID := "org_" + uuid.NewString()
	_, err := organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Platform MCP fixture organization", Slug: "platform-mcp-" + uuid.NewString()[:8], WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Platform MCP fixture project", Slug: "project-" + uuid.NewString()[:8], OrganizationID: organizationID})
	require.NoError(t, err)
	seedRegistrationEligibleCohort(t, ctx, conn, project.ID)
	client, err := platformrepo.New(conn).CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID: "client-" + uuid.NewString(), ClientSecretHash: pgtype.Text{}, ClientName: "Platform MCP fixture client", RedirectUris: []string{"https://client.test/callback"}, ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	connectionID, generation := uuid.New(), uuid.New()
	userID := "user_" + uuid.NewString()
	_, err = platformrepo.New(conn).CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{ID: connectionID, OrganizationID: organizationID, SubjectUrn: urn.NewUserSubject(userID).String(), OauthClientID: client.ID, ActiveGeneration: generation})
	require.NoError(t, err)
	return platformmcp.Principal{UserID: userID, OrganizationID: organizationID, ConnectionID: connectionID.String(), Generation: generation.String()}, platformmcp.ResolvedProject{ID: project.ID, Name: project.Name, Slug: project.Slug}
}

func seedRegistrationEligibleCohort(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) {
	t.Helper()

	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               "cohort-issuer-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	remote, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           "https://cohort.example.test/mcp",
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText("Registration cohort server"),
		Slug:                conv.ToPGText("cohort-server-" + uuid.NewString()[:8]),
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:   projectID,
		McpServerID: server.ID,
		Slug:        "cohort-endpoint-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
}

func registerReviewedMCP(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *platformmcp.RegistrationStore, principal platformmcp.Principal, project platformmcp.ResolvedProject, remoteURL string) platformrepo.PlatformMcpCatalogRegistration {
	t.Helper()
	request := platformmcp.CatalogRegistrationRequest{ProjectSlug: project.Slug, SourceKind: "catalog", CatalogProvider: "fixture", CatalogReference: "reviewed-fixture", IdempotencyKey: "reviewed-fixture-registration", InputHash: registrationInputHash(project.Slug, "catalog", "fixture", "reviewed-fixture")}
	receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
	require.NoError(t, err)
	receipt, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, request, receipt, remoteURL)
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, SourceKind: request.SourceKind, CatalogProvider: request.CatalogProvider, CatalogReference: request.CatalogReference})
	require.NoError(t, err)
	require.Equal(t, receipt.RegistrationID.UUID, registration.ID)
	return registration
}

func registrationInputHash(projectSlug, sourceKind, catalogProvider, catalogReference string) string {
	digest := sha256.Sum256([]byte(projectSlug + "\x00" + sourceKind + "\x00" + catalogProvider + "\x00" + catalogReference))
	return hex.EncodeToString(digest[:])
}

func seedReviewedRemoteIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, upstreamURL string) uuid.UUID {
	t.Helper()
	queries := remotesessionsrepo.New(conn)
	issuer, err := queries.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Slug: "reviewed-issuer-" + uuid.NewString()[:8], Issuer: upstreamURL,
		AuthorizationEndpoint: conv.ToPGText(upstreamURL + "/authorize"), TokenEndpoint: conv.ToPGText(upstreamURL + "/token"), RegistrationEndpoint: pgtype.Text{}, JwksUri: pgtype.Text{}, ScopesSupported: []string{"tools:read"}, GrantTypesSupported: []string{"authorization_code"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"none"}, Oidc: false, Passthrough: false,
	})
	require.NoError(t, err)
	return issuer.ID
}

func seedReviewedRemoteClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID string, userSessionIssuerID, remoteIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	queries := remotesessionsrepo.New(conn)
	client, err := queries.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(organizationID), RemoteSessionIssuerID: remoteIssuerID, ClientID: "reviewed-client", ClientSecretEncrypted: pgtype.Text{}, ClientIDIssuedAt: conv.ToPGTimestamptz(time.Now().UTC()), ClientSecretExpiresAt: pgtype.Timestamptz{}, TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{"tools:read"}, Audience: pgtype.Text{},
	})
	require.NoError(t, err)
	require.NoError(t, queries.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: userSessionIssuerID}))
	return client.ID
}

func providerProbeRequest(principal platformmcp.Principal, projectID uuid.UUID, registration platformrepo.PlatformMcpCatalogRegistration) platformmcp.ProviderReadinessProbeRequest {
	return platformmcp.ProviderReadinessProbeRequest{
		UserID: principal.UserID, OrganizationID: principal.OrganizationID, ProjectID: projectID, RegistrationID: registration.ID, UserSessionIssuerID: registration.UserSessionIssuerID.UUID, ConnectionID: uuid.MustParse(principal.ConnectionID), Generation: uuid.MustParse(principal.Generation),
	}
}

func visitProviderAuthorization(t *testing.T, upstream *reviewedUpstream, rawURL string) *url.URL {
	t.Helper()
	client := upstream.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(rawURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(t, http.StatusFound, response.StatusCode)
	callback, err := url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	return callback
}
