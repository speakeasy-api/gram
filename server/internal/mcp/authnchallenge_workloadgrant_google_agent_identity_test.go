package mcp_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// Synthetic native Agent Identity claims, not Google IAM service-account ID
// tokens. Only signing/key publication are local; exchange and admission use
// the real handler, repositories, replay guard, and authorization engine.
//
//nolint:paralleltest,tparallel // Negative cases share the fixture and must finish before successful exchanges create sessions and revoke its binding.
func TestWorkloadAssertionGrant_GoogleAgentIdentity(t *testing.T) {
	t.Parallel()
	const issuer = "https://sts.googleapis.com/v1/organizations/100000000001/locations/global/workloadIdentityPools/agents.global.org-100000000001.system.id.goog"
	const subject = "spiffe://agents.global.org-100000000001.system.id.goog/resources/container/projects/200000000002/locations/test-region/clusters/test-cluster/ns/runtime-test/sa/kit-runtime"
	f := newWorkloadGrantFixtureWithIdentity(t, testenv.NewLogger(t), "", nil, issuer, subject)
	require.LessOrEqual(t, len(subject), urn.MaxWorkloadExternalSubjectLength)
	// The production URL policy must accept the native STS path. Network/key
	// fetching below intentionally uses the fixture's HTTPS JWKS server.
	_, err := jwks.NewRemoteSource(issuer + "/jwks")
	require.NoError(t, err)
	mint := func(t *testing.T, sub, aud string) string {
		t.Helper()
		now := time.Now()
		return oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", jwt.Claims{
			Issuer: issuer, Subject: sub, Audience: jwt.Audience{aud},
			IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Hour)),
			// Native assertions have neither cnf nor jti.
		})
	}
	for _, tc := range []struct{ name, sub, aud string }{
		{"wrong KSA", subject + "-other", f.advertisedIssuer},
		{"wrong audience", subject, "https://other.example.com"},
		{"subject is not URL normalized", subject + "/", f.advertisedIssuer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireWorkloadGrantRefused(t, f.exchange(t, mint(t, tc.sub, tc.aud), f.resource))
			require.Empty(t, f.workloadSessions(t))
		})
	}
	assertion := mint(t, subject, f.advertisedIssuer)
	w := f.exchange(t, assertion, f.resource)
	f.requireWorkloadSession(t, w, f.advertisedIssuer)
	var response struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	admitted, _, _, err := f.ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), response.AccessToken, f.ti.serverURL.String(), workloadSessionEndpoint(f.fx))
	require.NoError(t, err)
	identity, ok := mcpidentity.FromContext(admitted)
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindAgent, identity.Kind())
	require.Equal(t, f.agentID.String(), identity.AgentID())
	auth, ok := contextvalues.GetAuthContext(admitted)
	require.True(t, ok)
	require.NotNil(t, auth.ProjectID)
	require.Equal(t, f.fx.target.ProjectID, *auth.ProjectID)
	require.Equal(t, f.fx.orgID, auth.ActiveOrganizationID)
	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, urn.NewWorkloadPrincipal(f.issuerID, subject).String(), actor.String())

	// No-jti platform tokens can be reused without the jti replay opt-in, but
	// every exchange and every admitted session must recheck the live binding.
	f.requireWorkloadSession(t, f.exchange(t, assertion, f.resource), f.advertisedIssuer)
	require.Len(t, f.workloadSessions(t), 2)
	unassignWorkloadAgent(t, f.ctx, f.ti, f.assignmentID)
	requireWorkloadGrantRefused(t, f.exchange(t, assertion, f.resource))
	_, _, _, err = f.ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), response.AccessToken, f.ti.serverURL.String(), workloadSessionEndpoint(f.fx))
	require.Error(t, err, "a previously issued session cannot survive binding revocation")
	require.Len(t, f.workloadSessions(t), 2)
}
