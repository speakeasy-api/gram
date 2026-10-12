package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// deviceAgentGrantFixture is the workload grant fixture with the same dev-idp
// keys also registered as the organization's Speakeasy device issuer, and the
// fixture's subject admitted and assigned under it.
type deviceAgentGrantFixture struct {
	workloadGrantFixture

	// deviceIssuer is the organization's device issuer URL.
	deviceIssuer string

	// deviceIssuerID is its workload issuer row.
	deviceIssuerID uuid.UUID

	// tokenURL is the device-agent token endpoint, the assertion audience.
	tokenURL string

	// assignmentID is the subject's agent assignment under the device issuer.
	assignmentID uuid.UUID
}

func newDeviceAgentGrantFixture(t *testing.T) deviceAgentGrantFixture {
	t.Helper()

	// Issuer URLs are https-only, and the device issuer is on the server URL.
	f := newWorkloadGrantFixtureOn(t, testenv.NewLogger(t), "https://speakeasy.example.test")
	ctx := t.Context()
	deviceIssuer := workloadidentity.DeviceIssuerURL(f.ti.serverURL.String(), f.fx.orgID)
	fixtures := testrepo.New(f.ti.conn)
	deviceIssuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: f.fx.orgID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "devices",
		Issuer:         deviceIssuer,
		JwksUri:        oauthtest.DiscoverWorkloadJWKSURI(t, f.issuer),
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID:   f.fx.orgID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: deviceIssuerID,
		Subject:          f.subject,
	}))
	assignmentID := assignAgentToWorkload(t, ctx, f.ti, f.fx.orgID, deviceIssuerID, f.subject, f.agentID)

	return deviceAgentGrantFixture{
		workloadGrantFixture: f,
		deviceIssuer:         deviceIssuer,
		deviceIssuerID:       deviceIssuerID,
		tokenURL:             deviceagenttoken.TokenURL(f.ti.serverURL.String()),
		assignmentID:         assignmentID,
	}
}

// deviceAssertion signs an assertion from the device issuer for subject,
// addressed to aud.
func (f deviceAgentGrantFixture) deviceAssertion(t *testing.T, subject, aud string) string {
	t.Helper()

	claims := oauthtest.WorkloadClaims(f.issuer, subject, aud)
	claims.Issuer = f.deviceIssuer
	return oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims)
}

func (f deviceAgentGrantFixture) exchangeDeviceAgent(t *testing.T, assertion string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", workloadGrantJWTBearer)
	form.Set("assertion", assertion)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, deviceagenttoken.TokenPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleDeviceAgentToken(w, req))
	return w
}

// An admitted device exchanges its assertion for a 15-minute device-agent
// token naming its workload, with no refresh token.
func TestDeviceAgentToken_AdmittedDeviceReceivesAToken(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	w := f.exchangeDeviceAgent(t, f.deviceAssertion(t, f.subject, f.tokenURL))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "Bearer", resp["token_type"])
	require.InDelta(t, float64(900), resp["expires_in"], 0)
	require.NotContains(t, resp, "refresh_token")

	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, []any{deviceagenttoken.Audience}, claims["aud"])
	require.Equal(t, urn.NewWorkloadPrincipal(f.deviceIssuerID, f.subject).String(), claims["sub"])
	require.Equal(t, f.fx.orgID, claims["org_id"])

	accessToken, ok := resp["access_token"].(string)
	require.True(t, ok)
	require.True(t, deviceagenttoken.IsToken(accessToken))
}

// An assertion addressed to another audience is refused.
func TestDeviceAgentToken_WrongAudienceRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.deviceAssertion(t, f.subject, f.advertisedIssuer)))
}

// A subject the organization has not admitted is refused.
func TestDeviceAgentToken_UnadmittedSubjectRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.deviceAssertion(t, "device:"+uuid.NewString(), f.tokenURL)))
}

// The same assertion cannot be exchanged twice.
func TestDeviceAgentToken_ReplayRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	assertion := f.deviceAssertion(t, f.subject, f.tokenURL)
	require.Equal(t, http.StatusOK, f.exchangeDeviceAgent(t, assertion).Code)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, assertion))
}

// A device whose assignment is withdrawn is refused on its next exchange.
func TestDeviceAgentToken_WithdrawnAssignmentRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	require.Equal(t, http.StatusOK, f.exchangeDeviceAgent(t, f.deviceAssertion(t, f.subject, f.tokenURL)).Code)

	unassignWorkloadAgent(t, t.Context(), f.ti, f.assignmentID)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.deviceAssertion(t, f.subject, f.tokenURL)))
}

// A device whose agent is suspended is refused.
func TestDeviceAgentToken_SuspendedAgentRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	_, err := agentsrepo.New(f.ti.conn).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
	require.NoError(t, err)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.deviceAssertion(t, f.subject, f.tokenURL)))
}

// A platform issuer's URL names no organization, so its assertions are
// refused here even when the subject is admitted and assigned.
func TestDeviceAgentToken_PlatformIssuerRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)))
}

// Only the JWT bearer grant is served.
func TestDeviceAgentToken_OtherGrantTypeRefused(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, deviceagenttoken.TokenPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleDeviceAgentToken(w, req))
	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "unsupported_grant_type", body["error"])
}
