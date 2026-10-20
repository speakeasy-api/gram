package deviceagenttoken_test

import (
	"encoding/base64"
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

	// A typ that is present but not a string is treated as this token, so it is
	// refused by the validator instead of passing for another credential.
	numericTyp := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":7}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".c2ln"
	require.True(t, deviceagenttoken.IsToken(numericTyp))
	untyped := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".c2ln"
	require.False(t, deviceagenttoken.IsToken(untyped))
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
	sync := authz.Check{Scope: authz.ScopeOrgDeviceAgentSync, ResourceKind: "", ResourceID: "org-123", Dimensions: nil}
	require.True(t, authz.GrantsSatisfy(grants, sync))
	require.False(t, authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeOrgDeviceAgentSync, ResourceKind: "", ResourceID: "org-other", Dimensions: nil}), "device sync must be scoped to its organization")

	// Hooks reach any project in the organization and none outside it. The
	// agent's own grants are what bound them to particular projects.
	require.True(t, authz.GrantsSatisfy(grants, authz.HooksIngestCheck("org-123", uuid.NewString())))
	require.False(t, authz.GrantsSatisfy(grants, authz.HooksIngestCheck("org-other", uuid.NewString())), "hooks must be scoped to its organization")

	require.True(t, authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: uuid.NewString(), Dimensions: nil}))
	require.False(t, authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeMCPConnect, ResourceKind: "", ResourceID: uuid.NewString(), Dimensions: nil}), "the ceiling must not reach MCP")
}

func TestCeiling_LeavesTheHooksProjectToTheAgentsGrant(t *testing.T) {
	t.Parallel()

	granted, other := uuid.NewString(), uuid.NewString()
	selector := authz.NewSelector(authz.ScopeOrgHooksIngest, "org-123")
	selector[authz.SelectorKeyProjectID] = granted
	agentGrants := []authz.Grant{authz.NewGrantWithSelector(authz.ScopeOrgHooksIngest, selector)}
	ceiling := deviceagenttoken.Ceiling("org-123")

	// A request passes only when both sets allow it, so the agent's
	// project-bounded grant is what decides.
	allowed := func(project string) bool {
		check := authz.HooksIngestCheck("org-123", project)
		return authz.GrantsSatisfy(ceiling, check) && authz.GrantsSatisfy(agentGrants, check)
	}
	require.True(t, allowed(granted))
	require.False(t, allowed(other))
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

// Any bearer presented on these routes is challenged when refused, including a
// bearer that is not a device-agent token and one wrapped in whitespace.
func TestBearerChallenge_ChallengesAnyRefusedBearer(t *testing.T) {
	t.Parallel()

	for _, authorization := range []string{
		"Bearer not-a-device-agent-token",
		"  bearer  " + mintToken(t) + "  ",
	} {
		handler := deviceagenttoken.BearerChallenge(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/rpc/agent.getPlugins", nil)
		req.Header.Set("Authorization", authorization)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, "Bearer", w.Header().Get("WWW-Authenticate"), authorization)
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
		{name: "another scheme", authorization: "Basic dXNlcjpwYXNz", status: http.StatusUnauthorized},
		{name: "empty bearer", authorization: "Bearer   ", status: http.StatusUnauthorized},
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
