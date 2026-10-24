package platformmcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/stretchr/testify/require"
)

func TestOAuthChallengeRejectsPrefixedStateWithoutConsumption(t *testing.T) {
	t.Parallel()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "connect"}, {http.MethodPost, "connect"},
		{http.MethodGet, "select-organization"}, {http.MethodPost, "select-organization"},
		{http.MethodGet, "idp_callback"},
		{http.MethodPost, "create-workspace"}, {http.MethodPost, "join-workspace"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			t.Parallel()
			ahp, _ := newAHPProtocolHTTP(t, []AHPProjectOption{{ID: "project-1", Slug: "default"}})
			form, _ := ahpProtocolConsent(t, ahp)
			state := form.Get("state")
			mcp := *ahp
			mcp.ahpProjects = nil
			mcp.workspaces = &workspaceTestState{}
			attack := url.Values{"state": {"ahp:" + state}, "csrf_token": {form.Get("csrf_token")}, "action": {"approve"}, "organization_id": {"org-1"}, "code": {"idp-code"}}
			target := "/platform-mcp/" + route.path
			req := httptest.NewRequest(route.method, target+"?"+attack.Encode(), strings.NewReader(attack.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handlers := map[string]http.Handler{"connect": mcp.ConnectHandler(), "select-organization": mcp.OrganizationSelectionHandler(), "idp_callback": mcp.IDPCallbackHandler(), "create-workspace": mcp.CreateWorkspaceHandler(), "join-workspace": mcp.JoinWorkspaceHandler()}
			response := httptest.NewRecorder()
			handlers[route.path].ServeHTTP(response, req)
			require.GreaterOrEqual(t, response.Code, 400)
			_, err := ahp.readChallenge(t.Context(), state, false)
			require.NoError(t, err, "foreign state must not consume the AHP challenge")
			valid := httptest.NewRecorder()
			ahp.ConnectHandler().ServeHTTP(valid, httptest.NewRequest(http.MethodGet, "/ahp/connect?state="+state, nil))
			require.Equal(t, http.StatusOK, valid.Code)
		})
	}
}

func TestOAuthChallengeChecksStoredNamespaceBeforeConsumption(t *testing.T) {
	t.Parallel()
	for _, consume := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "consume"}[consume], func(t *testing.T) {
			t.Parallel()
			s := newTestOAuthHTTP(t)
			raw := &memoryCache{values: map[string]any{}}
			s.cache = cache.NewTypedObjectCache[oauthChallenge](nil, raw, cache.SuffixNone)
			state := uuid.NewString()
			challenge := oauthChallenge{ID: state}
			require.NoError(t, s.cache.Store(t.Context(), challenge))
			challenge.Namespace = "ahp:"
			raw.mu.Lock()
			for key := range raw.values {
				raw.values[key] = challenge
			}
			raw.mu.Unlock()
			_, err := s.readChallenge(t.Context(), state, consume)
			require.ErrorIs(t, err, ErrUnauthorized)
			retained, err := s.cache.Get(t.Context(), s.challengePrefix()+state)
			require.NoError(t, err)
			require.Equal(t, challenge, retained)
		})
	}
}
