package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	jwksURI := oauthtest.DiscoverWorkloadJWKSURI(t, issuer)

	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardian.WithTLSRootCAs(issuer.RootCAs()))
	fx := newAgentConsentFixture(t, ctx, ti)
	ti.features.SetFlag(feature.FlagWorkloadAssertionGrant, fx.orgID, true)

	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	subject := "repo:acme/payments-api:ref:refs/heads/main"
	var issuerID uuid.UUID
	err := ti.conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `
		INSERT INTO workload_issuers (organization_id, name, issuer, jwks_uri)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, fx.orgID, "grant-issuer", issuer.OAuth21URL, jwksURI).Scan(&issuerID)
	require.NoError(t, err)
	_, err = ti.conn.Exec( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `
		INSERT INTO workload_identity_admissions (organization_id, workload_issuer_id, subject)
		VALUES ($1, $2, $3)
	`, fx.orgID, issuerID, subject)
	require.NoError(t, err)
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

	return oauthtest.MintWorkloadAssertion(t, f.issuer, oauthtest.WorkloadClaims(f.issuer, f.subject, aud))
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
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A genuine token from the admitted issuer for a subject this tenant never
// admitted is the multi-tenant case: the platform signs for every customer.
func TestWorkloadAssertionGrant_UnadmittedSubjectRefused(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)

	claims := oauthtest.WorkloadClaims(f.issuer, "repo:someone-else/app:ref:refs/heads/main", f.advertisedIssuer)
	w := f.exchange(t, oauthtest.MintWorkloadAssertion(t, f.issuer, claims), f.resource)
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
	w := f.exchange(t, oauthtest.MintWorkloadAssertion(t, f.issuer, claims), f.resource)
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

// With the grant's flag off, a clientless JWT bearer request gets the
// missing-client answer and mints nothing.
func TestWorkloadAssertionGrant_FlagOffAnswersAsBefore(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	f.ti.features.SetFlag(feature.FlagWorkloadAssertionGrant, f.fx.orgID, false)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_client")
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
	useAuthenticationHost(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID)
	harness := newAuthenticationHostHarness(t, f.ti)
	slug := f.fx.toolset.McpSlug.String
	authIssuer := testAuthenticationHostURL + "/mcp/" + slug

	form := url.Values{}
	form.Set("grant_type", workloadGrantJWTBearer)
	form.Set("assertion", f.assertion(t, authIssuer))
	form.Set("resource", f.resource)
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, authIssuer, accessTokenClaims(t, w.Body.Bytes())["iss"])

	// The authentication host's token URL is the endpoint half of the pair.
	form.Set("assertion", f.assertion(t, authenticationHostTokenURL(slug)))
	w = harness.serve(t, http.MethodPost, "auth.example.com", "/mcp/"+slug+"/token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
