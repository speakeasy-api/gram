package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestTokenAuthorizationCodeEnforcementOutageKeepsCode(t *testing.T) {
	t.Parallel()
	for _, agentHandoff := range []bool{false, true} {
		name := "user"
		if agentHandoff {
			name = "agent_handoff"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service, endpoint, clientRow, _ := newRefreshTokenReplayTestFixture(t, time.Now().Add(time.Hour))
			service.db = closedPool(t)
			rec := mcpconnectionattemptstest.New(t)
			service.connections = rec.Recorder
			mr := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			service.userSessionGrantCache = cache.NewTypedObjectCache[UserSessionGrant](testenv.NewLogger(t), cache.NewRedisCacheAdapter(client), cache.SuffixNone)
			verifier := "test-verifier"
			challenge := sha256.Sum256([]byte(verifier))
			grant := UserSessionGrant{
				Code:                "test-code",
				UserSessionIssuerID: endpoint.UserSessionIssuerID,
				UserSessionClientID: clientRow.ID,
				ClientID:            clientRow.ClientID,
				RedirectURI:         "https://client.example/callback",
				CodeChallenge:       base64.RawURLEncoding.EncodeToString(challenge[:]),
				CodeChallengeMethod: "S256",
				Subject:             urn.NewUserSubject("user_test"),
				CreatedAt:           time.Now(),
			}
			if agentHandoff {
				grant.Code = agentAuthorizationCodePrefix + grant.Code
				grant.AgentAuthorization = &AgentAuthorizationResult{}
			}
			require.NoError(t, service.userSessionGrantCache.Store(t.Context(), grant))
			request := httptest.NewRequest(http.MethodPost, "/mcp/test/token", nil)
			request.PostForm = url.Values{"code": {grant.Code}, "redirect_uri": {grant.RedirectURI}, "code_verifier": {verifier}}
			err := service.handleTokenAuthorizationCodeGrant(t.Context(), httptest.NewRecorder(), request, endpoint, clientRow, "https://example.com", "none", testenv.NewLogger(t))
			var failure *oops.ShareableError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, oops.CodeUnavailable, failure.Code)
			require.ErrorContains(t, err, "load upstream auth enforcement")
			saved, err := service.userSessionGrantCache.Get(t.Context(), grant.CacheKey())
			require.NoError(t, err, "an enforcement lookup outage must not consume the code")
			require.Equal(t, grant.Code, saved.Code)
		})
	}
}
