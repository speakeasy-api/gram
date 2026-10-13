package deviceagenttoken_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz/mcpauthztest"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func mintToken(t *testing.T) string {
	t.Helper()

	issuer := deviceagenttoken.New(mcpauthztest.NewIssuer(t), nil)
	token, expiresAt, err := issuer.Mint("org-123", urn.NewWorkloadPrincipal(uuid.New(), "device:"+uuid.NewString()), time.Now())
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(deviceagenttoken.Lifetime), expiresAt, time.Minute)
	return token
}

func TestMint_RefusesNonWorkloadPrincipals(t *testing.T) {
	t.Parallel()

	issuer := deviceagenttoken.New(mcpauthztest.NewIssuer(t), nil)
	_, _, err := issuer.Mint("org-123", urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()), time.Now())
	require.ErrorIs(t, err, deviceagenttoken.ErrInvalid)
	_, _, err = issuer.Mint("", urn.NewWorkloadPrincipal(uuid.New(), "device:x"), time.Now())
	require.ErrorIs(t, err, deviceagenttoken.ErrInvalid)
}

func TestIsToken_RecognizesOnlyDeviceAgentTokens(t *testing.T) {
	t.Parallel()

	require.True(t, deviceagenttoken.IsToken(mintToken(t)))
	require.False(t, deviceagenttoken.IsToken("not-a-jwt"))
	require.False(t, deviceagenttoken.IsToken(""))
}

func TestNew_NilSignerServesNothing(t *testing.T) {
	t.Parallel()

	require.Nil(t, deviceagenttoken.New(nil, nil))
}

func TestEncodeCeiling_CarriesTheOrganizationsDeviceAgentGrants(t *testing.T) {
	t.Parallel()

	encoded, version, err := deviceagenttoken.EncodeCeiling("org-123")
	require.NoError(t, err)
	policy, err := runtimepolicy.DecodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion(version), encoded)
	require.NoError(t, err)

	grants := policy.RuntimeGrants()
	for _, scope := range []authz.Scope{authz.ScopeOrgDeviceAgentSync, authz.ScopeOrgHooksIngest} {
		require.True(t, authz.GrantsSatisfy(grants, authz.Check{Scope: scope, ResourceKind: "", ResourceID: "org-123", Dimensions: nil}), scope)
		require.False(t, authz.GrantsSatisfy(grants, authz.Check{Scope: scope, ResourceKind: "", ResourceID: "org-other", Dimensions: nil}), "%s must be scoped to its organization", scope)
	}
	require.True(t, authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: uuid.NewString(), Dimensions: nil}))
	require.False(t, authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeMCPConnect, ResourceKind: "", ResourceID: uuid.NewString(), Dimensions: nil}), "the ceiling must not reach MCP")
}

func TestBearerChallenge_ChallengesARefusedDeviceAgentToken(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		refused   bool
		challenge string
	}{
		{name: "token refused", refused: true, challenge: `Bearer error="invalid_token"`},
		{name: "refused for another reason", refused: false, challenge: "Bearer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := deviceagenttoken.BearerChallenge(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.refused {
					deviceagenttoken.MarkInvalidToken(r.Context())
				}
				w.WriteHeader(http.StatusUnauthorized)
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/rpc/agent.getPlugins", nil)
			req.Header.Set("Authorization", "Bearer "+mintToken(t))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.Equal(t, tc.challenge, w.Header().Get("WWW-Authenticate"))
		})
	}
}

func TestBearerChallenge_LeavesOtherRequestsAlone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		authorization string
		status        int
	}{
		{name: "no authorization", authorization: "", status: http.StatusUnauthorized},
		{name: "other bearer", authorization: "Bearer not-a-device-agent-token", status: http.StatusUnauthorized},
		{name: "success", authorization: "Bearer " + mintToken(t), status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := deviceagenttoken.BearerChallenge(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/rpc/agent.getPlugins", nil)
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			require.Empty(t, w.Header().Get("WWW-Authenticate"))
		})
	}
}
