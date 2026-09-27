package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFederatedBrowserBinding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, cookie, hash string
		age                time.Duration
		wantError          bool
	}{
		{name: "bound browser", cookie: "browser-proof", hash: sha256Hex("browser-proof")},
		{name: "different browser", cookie: "other-browser", hash: sha256Hex("browser-proof"), wantError: true},
		{name: "missing cookie", hash: sha256Hex("browser-proof"), wantError: true},
		{name: "bootstrap not completed", cookie: "browser-proof", wantError: true},
		{name: "expired challenge", cookie: "browser-proof", hash: sha256Hex("browser-proof"), age: 11 * time.Minute, wantError: true},
		{name: "future challenge", cookie: "browser-proof", hash: sha256Hex("browser-proof"), age: -2 * time.Minute, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := AuthnChallengeState{ID: "challenge-id", CreatedAt: time.Now().Add(-test.age), Federation: &FederatedChallenge{BrowserHash: test.hash}}
			req := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/idp_callback", nil)
			if test.cookie != "" {
				req.AddCookie(&http.Cookie{Name: federationCookieName(state.ID), Value: test.cookie})
			}
			err := validateFederatedBrowser(req, state)
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFederatedBrowserCookieCannotCrossChallenges(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "https://gram.example/mcp/idp_callback", nil)
	req.AddCookie(&http.Cookie{Name: federationCookieName("another-challenge"), Value: "browser-proof"})
	require.Error(t, validateFederatedBrowser(req, AuthnChallengeState{ID: "challenge-id", CreatedAt: time.Now(), Federation: &FederatedChallenge{BrowserHash: sha256Hex("browser-proof")}}))
	require.Error(t, validateFederatedBrowser(req, AuthnChallengeState{}))
}
