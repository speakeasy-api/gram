package mcp_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
	workloadpolicyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// deviceAgentGrantFixture is the workload grant fixture on an https server
// URL, which device issuer URLs require, with the platform subject admitted
// and assigned in the organization.
type deviceAgentGrantFixture struct {
	workloadGrantFixture

	// tokenURL is the organization's device-agent token endpoint, the
	// audience its assertions must name.
	tokenURL string
}

func newDeviceAgentGrantFixture(t *testing.T) deviceAgentGrantFixture {
	t.Helper()

	f := newWorkloadGrantFixtureOn(t, testenv.NewLogger(t), "https://speakeasy.example.test")
	return deviceAgentGrantFixture{
		workloadGrantFixture: f,
		tokenURL:             deviceagenttoken.TokenURL(f.ti.serverURL.String(), f.fx.orgID),
	}
}

// exchangeDeviceAgentAt posts assertion to organizationID's device-agent
// token endpoint.
func (f deviceAgentGrantFixture) exchangeDeviceAgentAt(t *testing.T, organizationID, assertion string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", workloadGrantJWTBearer)
	form.Set("assertion", assertion)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, deviceagenttoken.TokenPath(organizationID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(deviceagenttoken.TokenPathOrganizationParam, organizationID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleDeviceAgentToken(w, req))
	return w
}

func (f deviceAgentGrantFixture) exchangeDeviceAgent(t *testing.T, assertion string) *httptest.ResponseRecorder {
	t.Helper()
	return f.exchangeDeviceAgentAt(t, f.fx.orgID, assertion)
}

// requireDeviceAgentToken asserts w carries a 15-minute device-agent token
// naming the workload principal for issuerID and subject.
func requireDeviceAgentToken(t *testing.T, w *httptest.ResponseRecorder, organizationID string, issuerID uuid.UUID, subject string) {
	t.Helper()

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "Bearer", resp["token_type"])
	require.InDelta(t, float64(900), resp["expires_in"], 0)
	require.NotContains(t, resp, "refresh_token")

	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, []any{deviceagenttoken.Audience}, claims["aud"])
	require.Equal(t, urn.NewWorkloadPrincipal(issuerID, subject).String(), claims["sub"])
	require.Equal(t, organizationID, claims["org_id"])

	accessToken, ok := resp["access_token"].(string)
	require.True(t, ok)
	require.True(t, deviceagenttoken.IsToken(accessToken))
}

// registeredDevice is a device enrolled through registerDevice.
type registeredDevice struct {
	registration *gen.WorkloadDeviceRegistration
	key          *ecdsa.PrivateKey
	kid          string
}

// registerDevice enrolls a fresh key the way a device does: a join token for
// the fixture's agent, created by its owner, redeemed by registerDevice.
func (f deviceAgentGrantFixture) registerDevice(t *testing.T) registeredDevice {
	t.Helper()

	policy := f.workloadPolicy(t)

	// A join token is refused for an agent that could not poll. The agent gets
	// no hooks grant: enrolling does not need one.
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	seedPrincipalGrant(t, f.ctx, f.ti, f.fx.orgID, agentPrincipal, authz.ScopeOrgDeviceAgentSync, f.fx.orgID)

	join, err := policy.CreateDeviceJoinToken(f.ctx, &gen.CreateDeviceJoinTokenPayload{
		AgentID:          f.agentID.String(),
		ExpiresInSeconds: 600,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	public := jose.JSONWebKey{Key: key.Public(), KeyID: "", Algorithm: "", Use: ""}
	sum, err := public.Thumbprint(crypto.SHA256)
	require.NoError(t, err)
	document, err := public.MarshalJSON()
	require.NoError(t, err)
	var jwk map[string]any
	require.NoError(t, json.Unmarshal(document, &jwk))

	hostname := "vps-test"
	registration, err := policy.RegisterDevice(t.Context(), &gen.RegisterDevicePayload{
		JoinToken: join.Token,
		PublicJwk: jwk,
		Hostname:  &hostname,
		Serial:    nil,
	})
	require.NoError(t, err)
	return registeredDevice{registration: registration, key: key, kid: base64.RawURLEncoding.EncodeToString(sum)}
}

// workloadPolicy is the workload identity service the dashboard calls, on the
// fixture's database and server URL.
func (f deviceAgentGrantFixture) workloadPolicy(t *testing.T) *workloadpolicy.Service {
	t.Helper()

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return workloadpolicy.NewService(f.ti.logger, f.ti.tracerProvider, f.ti.conn, f.ti.sessionManager, f.ti.authzEngine, f.ti.audit,
		authserver.Hosts{ServerURL: f.ti.serverURL.String(), AuthenticationHostBaseURL: "", PlatformHosts: nil}, ratelimit.NewRedisStore(redisClient), cache.NewRedisCacheAdapter(redisClient), &feature.InMemory{})
}

// assertion signs an assertion as the device, addressed to aud.
func (d registeredDevice) assertion(t *testing.T, aud string) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: d.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), d.kid),
	)
	require.NoError(t, err)
	now := time.Now()
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer:   d.registration.Issuer,
		Subject:  d.registration.DeviceSubject,
		Audience: jwt.Audience{aud},
		ID:       uuid.NewString(),
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(5 * time.Minute)),
	}).Serialize()
	require.NoError(t, err)
	return raw
}

// deviceIssuerID returns the organization's device issuer row, the one a
// registered device's assertions resolve to.
func (f deviceAgentGrantFixture) deviceIssuerID(t *testing.T, d registeredDevice) uuid.UUID {
	t.Helper()

	issuers, err := workloadpolicyrepo.New(f.ti.conn).FindDeviceIssuer(t.Context(), f.fx.orgID)
	require.NoError(t, err)
	require.Len(t, issuers, 1)
	require.Equal(t, d.registration.Issuer, issuers[0].Issuer)
	return issuers[0].ID
}

// A platform workload admitted in the organization exchanges at that
// organization's endpoint, the path naming the tenant its issuer cannot.
func TestDeviceAgentToken_PlatformWorkloadReceivesAToken(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	requireDeviceAgentToken(t, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)), f.fx.orgID, f.issuerID, f.subject)
}

// A device enrolled through registerDevice exchanges an assertion signed with
// its registered key.
func TestDeviceAgentToken_RegisteredDeviceReceivesAToken(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	device := f.registerDevice(t)
	require.Equal(t, f.fx.orgID, device.registration.OrganizationID)
	requireDeviceAgentToken(t, f.exchangeDeviceAgent(t, device.assertion(t, f.tokenURL)), f.fx.orgID, f.deviceIssuerID(t, device), device.registration.DeviceSubject)
}

// A device whose agent holds no hooks grant still enrolls and receives a token,
// so it can use MCP; each hooks request it makes is refused by RBAC.
func TestDeviceAgentToken_DeviceWithoutHooksGrantGetsATokenButNoHooks(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	device := f.registerDevice(t)
	w := f.exchangeDeviceAgent(t, device.assertion(t, f.tokenURL))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	token, ok := resp["access_token"].(string)
	require.True(t, ok)

	callerIssuer, err := sharedCallerIssuer()
	require.NoError(t, err)
	admitted, _, err := deviceagenttoken.New(callerIssuer, f.ti.conn).Admit(t.Context(), token, f.ti.authzEngine)
	require.NoError(t, err)

	err = f.ti.authzEngine.Require(admitted, authz.HooksIngestCheck(f.fx.orgID, f.fx.target.ProjectID.String()))
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, "error: %v", err)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}

// A device cannot exchange at another organization's endpoint, even with the
// audience that endpoint expects.
func TestDeviceAgentToken_DeviceAtAnotherOrganizationRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	device := f.registerDevice(t)
	other := seedOtherOrganization(t, f)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgentAt(t, other, device.assertion(t, deviceagenttoken.TokenURL(f.ti.serverURL.String(), other))))
}

// A platform subject admitted in one organization is refused at another
// organization's endpoint, which has not admitted it.
func TestDeviceAgentToken_PlatformWorkloadAtAnotherOrganizationRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	other := seedOtherOrganization(t, f)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgentAt(t, other, f.assertion(t, deviceagenttoken.TokenURL(f.ti.serverURL.String(), other))))
}

// An organization that does not exist reads like any other refusal.
func TestDeviceAgentToken_UnknownOrganizationRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	unknown := "org-" + uuid.NewString()
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgentAt(t, unknown, f.assertion(t, deviceagenttoken.TokenURL(f.ti.serverURL.String(), unknown))))
}

// An assertion addressed to another endpoint, this organization's MCP
// authorization server or another organization's device-agent endpoint, is
// refused.
func TestDeviceAgentToken_WrongAudienceRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, f.advertisedIssuer)))
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, deviceagenttoken.TokenURL(f.ti.serverURL.String(), "org-elsewhere"))))
}

// A subject the organization has not admitted is refused.
func TestDeviceAgentToken_UnadmittedSubjectRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	f.subject = "repo:acme/unadmitted:ref:refs/heads/main"
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)))
}

// The same assertion cannot be exchanged twice.
func TestDeviceAgentToken_ReplayRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	assertion := f.assertion(t, f.tokenURL)
	require.Equal(t, http.StatusOK, f.exchangeDeviceAgent(t, assertion).Code)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, assertion))
}

// A workload whose assignment is withdrawn is refused on its next exchange.
func TestDeviceAgentToken_WithdrawnAssignmentRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	require.Equal(t, http.StatusOK, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)).Code)

	unassignWorkloadAgent(t, t.Context(), f.ti, f.assignmentID)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)))
}

// Revoking a device refuses a token it was already issued on that token's next
// request, not only its next exchange: revocation withdraws the admission,
// assignment and key together, and every request re-reads the assignment.
func TestDeviceAgentToken_IssuedTokenRefusedAfterDeviceRevoked(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	device := f.registerDevice(t)
	w := f.exchangeDeviceAgent(t, device.assertion(t, f.tokenURL))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	token, ok := resp["access_token"].(string)
	require.True(t, ok)

	callerIssuer, err := sharedCallerIssuer()
	require.NoError(t, err)
	tokens := deviceagenttoken.New(callerIssuer, f.ti.conn)
	_, agent, err := tokens.Admit(t.Context(), token, f.ti.authzEngine)
	require.NoError(t, err, "the token is admitted before revocation")
	require.Equal(t, f.agentID.String(), agent.Principal.ID)

	policy := f.workloadPolicy(t)
	listed, err := policy.List(f.ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	var admissionID string
	for _, admission := range listed.Admissions {
		if admission.Subject == device.registration.DeviceSubject {
			admissionID = admission.ID
		}
	}
	require.NotEmpty(t, admissionID)
	_, err = policy.WithdrawSubject(f.ctx, &gen.WithdrawSubjectPayload{ID: admissionID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	_, _, err = tokens.Admit(t.Context(), token, f.ti.authzEngine)
	require.Error(t, err, "the already-issued token is refused on its next request")
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, device.assertion(t, f.tokenURL)))
}

// A workload whose agent is suspended is refused.
func TestDeviceAgentToken_SuspendedAgentRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	_, err := agentsrepo.New(f.ti.conn).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
	require.NoError(t, err)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)))
}

// A disabled organization is issued no token, since every request it made
// with one would be refused.
func TestDeviceAgentToken_DisabledOrganizationRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	_, err := adminrepo.New(f.ti.conn).AdminDisableOrganization(t.Context(), f.fx.orgID)
	require.NoError(t, err)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)))
}

// Only the JWT bearer grant is served.
func TestDeviceAgentToken_OtherGrantTypeRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, deviceagenttoken.TokenPath(f.fx.orgID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleDeviceAgentToken(w, req))
	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "unsupported_grant_type", body["error"])
}

// seedOtherOrganization creates a second organization with the agent rollout
// enabled, so a refusal there is down to tenancy rather than the rollout.
func seedOtherOrganization(t *testing.T, f deviceAgentGrantFixture) string {
	t.Helper()

	id := "org-" + uuid.NewString()
	require.NoError(t, testrepo.New(f.ti.conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 id,
		Name:               "Other organization",
		Slug:               "other-" + uuid.NewString()[:8],
		GramAccountType:    "enterprise",
		WorkosID:           pgtype.Text{},
		Whitelisted:        false,
		FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		DisabledAt:         pgtype.Timestamptz{},
		CreatedAt:          pgtype.Timestamptz{},
	}))
	return id
}
