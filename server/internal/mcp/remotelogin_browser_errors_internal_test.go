package mcp

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/stretchr/testify/require"
)

func TestRemoteLoginBrowserCacheFailures(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"remote", "parent", "confirmation", "bind", "owner"} {
		for _, unavailable := range []bool{false, true} {
			name := "missing"
			if unavailable {
				name = "unavailable"
			}
			t.Run(stage+"/"+name, func(t *testing.T) {
				t.Parallel()
				s := browserTestService(t)
				var logs bytes.Buffer
				s.logger = slog.New(slog.NewJSONHandler(&logs, nil))
				parent := browserTestState()
				remote := remotesessions.RemoteLoginState{ID: uuid.NewString(), ParentChallengeID: parent.ID, Subject: parent.Subject, UserSessionIssuerID: parent.UserSessionIssuerID, BrowserCookieID: parent.Browser.CookieID, BrowserHash: sha256Hex("remote-browser")}
				hop := remoteLoginHop{ID: uuid.NewString(), Phase: remoteLoginHopConfirm, ParentChallengeID: parent.ID, CreatedAt: time.Now()}
				if stage != "remote" {
					require.NoError(t, s.remoteLoginCache.Store(t.Context(), remote))
				}
				if stage != "parent" && stage != "owner" {
					require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
				}
				if stage == "owner" {
					require.NoError(t, s.remoteLoginHopCache.Store(t.Context(), hop))
				}
				if unavailable {
					switch stage {
					case "remote":
						s.remoteLoginCache = cache.NewTypedObjectCache[remotesessions.RemoteLoginState](s.logger, nil, cache.SuffixNone)
					case "parent", "owner":
						s.authnChallengeCache = cache.NewTypedObjectCache[AuthnChallengeState](s.logger, nil, cache.SuffixNone)
					case "confirmation", "bind":
						s.remoteLoginHopCache = cache.NewTypedObjectCache[remoteLoginHop](s.logger, nil, cache.SuffixNone)
					}
				}
				request := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?state="+remote.ID, nil)
				request.AddCookie(federatedBrowserCookie(remote.BrowserCookieID, "remote-browser", 600))
				handler := func(_ http.ResponseWriter, r *http.Request) error { return s.validateRemoteLoginBrowser(r) }
				if stage == "bind" || stage == "owner" {
					request = httptest.NewRequest(http.MethodGet, "/mcp/remote_login_bind?state="+hop.ID, nil)
					request.AddCookie(federatedBrowserCookie(parent.Browser.CookieID, "callback-browser", 600))
					handler = s.HandleRemoteLoginBind
				}
				response := httptest.NewRecorder()
				oops.BrowserErrHandle(s.logger, handler).ServeHTTP(response, request)
				status := http.StatusUnauthorized
				message := "This connection link expired or was already used."
				if unavailable {
					status = http.StatusServiceUnavailable
					message = "This is a problem on our side"
					require.Contains(t, logs.String(), "remote login state is temporarily unavailable")
				}
				require.Equal(t, status, response.Code)
				require.Contains(t, response.Body.String(), message)
				require.NotContains(t, response.Body.String(), "match your current browser")
				require.Empty(t, response.Header().Get("Location"))
			})
		}
	}
}
