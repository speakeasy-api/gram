package mcp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const workloadGrantJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// workloadGrantFixture is an issuer-gated MCP server whose organization has
// the grant and the agent authorization rollout on, a dev-idp trusted as a
// workload issuer at the organization tier, an admitted subject, and an agent
// assigned to it that may connect to the server.
type workloadGrantFixture struct {
	ti       *testInstance
	fx       agentConsentFixture
	issuer   *devidptest.Instance
	issuerID uuid.UUID
	agentID  uuid.UUID
	subject  string

	// resource is the server's canonical URL on the MCP host.
	resource string
	// advertisedIssuer is the issuer the server's metadata names.
	advertisedIssuer string
}

func newWorkloadGrantFixture(t *testing.T) workloadGrantFixture {
	t.Helper()

	return newWorkloadGrantFixtureWithLogger(t, testenv.NewLogger(t))
}

// newWorkloadGrantFixtureWithLogger builds the fixture with every service log
// line written to logger.
func newWorkloadGrantFixtureWithLogger(t *testing.T, logger *slog.Logger) workloadGrantFixture {
	t.Helper()

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	jwksURI := oauthtest.DiscoverWorkloadJWKSURI(t, issuer)

	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, logger, testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     0,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	}, nil, guardian.WithTLSRootCAs(issuer.RootCAs()))
	fx := newAgentConsentFixture(t, ctx, ti)

	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	subject := "repo:acme/payments-api:ref:refs/heads/main"
	fixtures := testrepo.New(ti.conn)
	issuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: fx.orgID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "grant-issuer",
		Issuer:         issuer.OAuth21URL,
		JwksUri:        jwksURI,
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID:   fx.orgID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuerID,
		Subject:          subject,
	}))
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, subject, agent.ID)

	slug := fx.toolset.McpSlug.String
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, slug)

	return workloadGrantFixture{
		ti:               ti,
		fx:               fx,
		issuer:           issuer,
		issuerID:         issuerID,
		agentID:          agent.ID,
		subject:          subject,
		resource:         strings.TrimSuffix(ti.serverURL.String(), "/") + "/mcp/" + slug,
		advertisedIssuer: advertisedIssuer,
	}
}

// assertion signs a fresh assertion for the admitted subject addressed to aud.
func (f workloadGrantFixture) assertion(t *testing.T, aud string) string {
	t.Helper()

	return oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", oauthtest.WorkloadClaims(f.issuer, f.subject, aud))
}

// exchange posts the clientless JWT bearer grant to the MCP host.
func (f workloadGrantFixture) exchange(t *testing.T, assertion string, resources ...string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", workloadGrantJWTBearer)
	form.Set("assertion", assertion)
	for _, resource := range resources {
		form.Add("resource", resource)
	}
	return postForm(t, f.ti, f.fx.toolset.McpSlug.String, "token", form)
}

// workloadSessions lists the sessions minted for the fixture's workload.
func (f workloadGrantFixture) workloadSessions(t *testing.T) []usersessionsrepo.ListUserSessionsByProjectIDRow {
	t.Helper()

	rows, err := usersessionsrepo.New(f.ti.conn).ListUserSessionsByProjectID(t.Context(), usersessionsrepo.ListUserSessionsByProjectIDParams{
		ProjectID:           f.fx.target.ProjectID,
		OrganizationID:      f.fx.orgID,
		Status:              pgtype.Text{String: "", Valid: false},
		SubjectUrn:          pgtype.Text{String: urn.NewWorkloadSubject(f.issuerID, f.subject).String(), Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ID:                  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Cursor:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:          10,
	})
	require.NoError(t, err)
	return rows
}

// requireWorkloadSession asserts an exchange minted a workload session for the
// fixture's subject, with no refresh token and the given issuer, and that the
// MCP side admits it.
func (f workloadGrantFixture) requireWorkloadSession(t *testing.T, w *httptest.ResponseRecorder, wantIssuer string) {
	t.Helper()

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotContains(t, resp, "refresh_token")

	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, wantIssuer, claims["iss"])
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), claims["sub"])

	accessToken, ok := resp["access_token"].(string)
	require.True(t, ok)
	_, _, _, err := f.ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), accessToken, f.ti.serverURL.String(), workloadSessionEndpoint(f.fx))
	require.NoError(t, err, "the MCP side must admit the session the grant minted")
}

func requireWorkloadGrantRefused(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "invalid_grant", body["error"])
	require.Equal(t, "assertion is invalid", body["error_description"], "every refusal must read the same on the wire")
}

// An admitted workload with a live assigned agent exchanges its platform token
// for a resource-scoped session with no refresh token, and the MCP side admits
// that session on its first request.
func TestWorkloadAssertionGrant_AdmittedWorkloadReceivesASession(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotContains(t, resp, "refresh_token")
	expiresIn, ok := resp["expires_in"].(float64)
	require.True(t, ok)
	require.GreaterOrEqual(t, expiresIn, float64(300))
	require.LessOrEqual(t, expiresIn, float64(86400))

	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, f.advertisedIssuer, claims["iss"])
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), claims["sub"])

	sessions := f.workloadSessions(t)
	require.Len(t, sessions, 1)
	session := sessions[0]
	stored, err := usersessionsrepo.New(f.ti.conn).GetUserSessionByID(t.Context(), usersessionsrepo.GetUserSessionByIDParams{
		ID:             session.ID,
		ProjectID:      f.fx.target.ProjectID,
		OrganizationID: f.fx.orgID,
	})
	require.NoError(t, err)
	require.False(t, stored.UserSessionClientID.Valid, "a workload session has no client")
	require.False(t, stored.AuthorizerUserID.Valid, "a workload session records no approving human")
	require.False(t, stored.RefreshTokenHash.Valid, "a workload session stores no refresh token hash")
	require.True(t, stored.RefreshExpiresAt.Time.Equal(stored.ExpiresAt.Time), "the session is live exactly as long as its access token")

	accessToken, ok := resp["access_token"].(string)
	require.True(t, ok)
	_, _, _, err = f.ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), accessToken, f.ti.serverURL.String(), workloadSessionEndpoint(f.fx))
	require.NoError(t, err, "the MCP side must admit the session the grant minted")
}

// An assertion may name the token endpoint URL instead of the issuer.
func TestWorkloadAssertionGrant_TokenEndpointAudienceAccepted(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer+"/token"), f.resource)
	f.requireWorkloadSession(t, w, f.advertisedIssuer)
	require.Len(t, f.workloadSessions(t), 1)
}

// A genuine token from the admitted issuer for a subject this tenant never
// admitted is the multi-tenant case: the platform signs for every customer.
func TestWorkloadAssertionGrant_UnadmittedSubjectRefused(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)

	claims := oauthtest.WorkloadClaims(f.issuer, "repo:someone-else/app:ref:refs/heads/main", f.advertisedIssuer)
	w := f.exchange(t, oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims), f.resource)
	requireWorkloadGrantRefused(t, w)
}

// Without a live assigned agent the MCP side would refuse the session, so the
// grant refuses first and writes no row.
func TestWorkloadAssertionGrant_SuspendedAgentRefused(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	_, err := agentsrepo.New(f.ti.conn).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
	require.NoError(t, err)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	requireWorkloadGrantRefused(t, w)
	require.Empty(t, f.workloadSessions(t), "no session row is written for a refused workload")
}

// The same assertion cannot be exchanged twice.
func TestWorkloadAssertionGrant_ReplayRefused(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	assertion := f.assertion(t, f.advertisedIssuer)

	w := f.exchange(t, assertion, f.resource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = f.exchange(t, assertion, f.resource)
	requireWorkloadGrantRefused(t, w)
}

// A token valid at several audiences is not minted for this endpoint alone.
func TestWorkloadAssertionGrant_MultipleAudiencesRefused(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)

	claims := oauthtest.WorkloadClaims(f.issuer, f.subject, f.advertisedIssuer)
	claims.Audience = jwt.Audience{f.advertisedIssuer, "https://elsewhere.example.com"}
	w := f.exchange(t, oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims), f.resource)
	requireWorkloadGrantRefused(t, w)
}

// An audience naming another URL is refused.
func TestWorkloadAssertionGrant_OtherAudienceRefused(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer+"/revoke"), f.resource)
	requireWorkloadGrantRefused(t, w)
}

// resource is required and must name this server.
func TestWorkloadAssertionGrant_ResourceRequiredAndBound(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_request")

	w = f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource+"-other")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_target")
}

// Without the agent authorization rollout, a clientless JWT bearer request
// gets the missing-client answer and mints nothing: a workload acts through
// its assigned agent's policy, which the MCP side honours only under that
// rollout.
func TestWorkloadAssertionGrant_RolloutOffAnswersAsBefore(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	f.ti.features.SetFlag(feature.FlagAgentMCPAuthorizationM2, f.fx.orgID, false)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_grant")
	require.Empty(t, f.workloadSessions(t))
}

// With the agent authorization rollout off, the grant is refused even with its
// own flag on: the MCP side would refuse every session it minted.
func TestWorkloadAssertionGrant_AgentRolloutOffRefused(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	f.ti.features.SetFlag(feature.FlagAgentMCPAuthorizationM2, f.fx.orgID, false)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	requireWorkloadGrantRefused(t, w)
	require.Empty(t, f.workloadSessions(t))
}

// An issuer that opts in to the authentication host mints sessions whose iss
// is the authentication host issuer its metadata names, and an assertion
// addressed to that issuer is accepted.
func TestWorkloadAssertionGrant_AuthenticationHostIssuer(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	useAuthenticationHost(t, t.Context(), f.ti, f.fx.orgID, f.fx.target.UserSessionIssuerID)
	harness := newAuthenticationHostHarness(t, f.ti)
	slug := f.fx.toolset.McpSlug.String
	authIssuer := testAuthenticationHostURL + "/mcp/" + slug

	form := url.Values{}
	form.Set("grant_type", workloadGrantJWTBearer)
	form.Set("assertion", f.assertion(t, authIssuer))
	form.Set("resource", f.resource)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	f.requireWorkloadSession(t, w, authIssuer)

	// The authentication host's token URL is the endpoint half of the pair.
	form.Set("assertion", f.assertion(t, authenticationHostTokenURL(slug)))
	w = harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	f.requireWorkloadSession(t, w, authIssuer)
}

// syncBuffer is a bytes.Buffer safe for the concurrent writes a service's
// logger makes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write log buffer: %w", err)
	}
	return n, nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Neither the assertion nor the minted access token reaches any log line, on
// the success path or any refusal. The refusal line names the presented
// subject and a reason; the success line records the issuance.
func TestWorkloadAssertionGrant_NoTokenBytesInLogs(t *testing.T) {
	t.Parallel()

	var logs syncBuffer
	f := newWorkloadGrantFixtureWithLogger(t, slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false, ReplaceAttr: nil})))

	var secrets []string
	present := func(assertion string) *httptest.ResponseRecorder {
		secrets = append(secrets, assertion)
		// Each JWS segment is checked on its own too, so a log line carrying
		// part of a token is still caught.
		secrets = append(secrets, strings.Split(assertion, ".")...)
		return f.exchange(t, assertion, f.resource)
	}

	issued := present(f.assertion(t, f.advertisedIssuer))
	require.Equal(t, http.StatusOK, issued.Code, issued.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &resp))
	accessToken, ok := resp["access_token"].(string)
	require.True(t, ok)
	secrets = append(secrets, accessToken)

	// A replay, a subject never admitted, and an audience naming another URL.
	requireWorkloadGrantRefused(t, present(secrets[0]))
	outsider := "repo:someone-else/app:ref:refs/heads/main"
	requireWorkloadGrantRefused(t, present(oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", oauthtest.WorkloadClaims(f.issuer, outsider, f.advertisedIssuer))))
	requireWorkloadGrantRefused(t, present(f.assertion(t, f.advertisedIssuer+"/revoke")))

	written := logs.String()
	for _, secret := range secrets {
		require.NotContains(t, written, secret)
	}
	require.Contains(t, written, "workload session issued")
	require.Contains(t, written, "workload assertion grant refused")
	require.Contains(t, written, outsider, "the refusal line names the presented subject so an operator can admit it")
	require.Contains(t, written, "subject_not_admitted")
	require.Contains(t, written, "assertion_replayed")
}

// A cold exchange, which fetches the issuer's key set before admitting the
// workload, completes well inside the roughly ten seconds Claude Tag waits.
//
// Not parallel: the measurement must not compete with the package's other
// fixtures for the same database and CPU.
func TestWorkloadAssertionGrant_ColdExchangeFitsTheClientTimeout(t *testing.T) { //nolint:paralleltest // wall-clock measurement

	f := newWorkloadGrantFixture(t)

	started := time.Now()
	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	elapsed := time.Since(started)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Less(t, elapsed, 5*time.Second, "a cold exchange must leave headroom under a 10 s client timeout")
}

// The grant is advertised in the authorization server metadata only where it
// would be accepted.
func TestWorkloadAssertionGrant_AdvertisedOnlyWhenAccepted(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	slug := f.fx.toolset.McpSlug.String

	advertised := func() []any {
		grants, ok := fetchASMetadata(t, f.ti, slug)["grant_types_supported"].([]any)
		require.True(t, ok)
		return grants
	}

	require.ElementsMatch(t, []any{"authorization_code", "refresh_token", workloadGrantJWTBearer}, advertised())

	f.ti.features.SetFlag(feature.FlagAgentMCPAuthorizationM2, f.fx.orgID, false)
	require.ElementsMatch(t, []any{"authorization_code", "refresh_token"}, advertised(), "not advertised while the agent rollout is off")

	f.ti.features.SetFlag(feature.FlagAgentMCPAuthorizationM2, f.fx.orgID, true)
	require.ElementsMatch(t, []any{"authorization_code", "refresh_token", workloadGrantJWTBearer}, advertised(), "advertised again once the rollout is back on")
}
