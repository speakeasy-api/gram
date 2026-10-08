package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
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

// The IdP callback recorded on a challenge is accepted on whichever origin it
// was minted with, but only as the client's per-client HTTPS callback.
func TestRecordedIDPCallbackOrigin(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	for _, test := range []struct {
		callback, want string
		wantErr        bool
	}{
		{callback: "https://reg.example/mcp/idp_callback/" + clientID.String(), want: "https://reg.example"},
		{callback: "https://app.example/mcp/idp_callback/" + clientID.String(), want: "https://app.example"},
		{callback: "https://reg.example/mcp/idp_callback", wantErr: true},
		{callback: "https://reg.example/x/mcp/idp_callback", wantErr: true},
		{callback: "https://reg.example/mcp/idp_callback/" + uuid.NewString(), wantErr: true},
		{callback: "https://reg.example/x/mcp/idp_callback/" + clientID.String(), wantErr: true},
		{callback: "https://reg.example/mcp/idp_callback/" + clientID.String() + "/", wantErr: true},
		{callback: "http://reg.example/mcp/idp_callback/" + clientID.String(), wantErr: true},
		{callback: "https://reg.example/mcp/idp_callback/" + clientID.String() + "?x=1", wantErr: true},
		{callback: "", wantErr: true},
	} {
		origin, err := recordedIDPCallbackOrigin(test.callback, clientID)
		if test.wantErr {
			require.Error(t, err, test.callback)
			continue
		}
		require.NoError(t, err, test.callback)
		require.Equal(t, test.want, origin.String())
	}
	_, err := federatedIDPCallbackURL(&url.URL{Scheme: "https", Host: "reg.example"}, uuid.Nil)
	require.Error(t, err)
}

// A federated callback completes only on exactly its recorded per-client path.
func TestFederatedCallbackRoute(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	perClient := "https://reg.example/mcp/idp_callback/" + clientID.String()
	for _, test := range []struct {
		name                    string
		callback, path, routeID string
		wantErr                 bool
	}{
		{name: "per-client on its path", callback: perClient, path: "/mcp/idp_callback/" + clientID.String(), routeID: clientID.String()},
		{name: "per-client on shared path", callback: perClient, path: "/mcp/idp_callback", wantErr: true},
		{name: "per-client on slug path", callback: perClient, path: "/mcp/petstore/idp_callback", wantErr: true},
		{name: "per-client with other route id", callback: perClient, path: "/mcp/idp_callback/" + clientID.String(), routeID: uuid.NewString(), wantErr: true},
		{name: "per-client with empty route id", callback: perClient, path: "/mcp/idp_callback/" + clientID.String(), wantErr: true},
		{name: "per-client route id mismatches path", callback: perClient, path: "/x/mcp/idp_callback/" + clientID.String(), routeID: clientID.String(), wantErr: true},
		{name: "per-client suffix on another prefix", callback: perClient, path: "/other/mcp/idp_callback/" + clientID.String(), routeID: clientID.String(), wantErr: true},
		{name: "shared callback on shared path", callback: "https://reg.example/mcp/idp_callback", path: "/mcp/idp_callback", wantErr: true},
		{name: "missing callback", path: "/mcp/idp_callback/" + clientID.String(), routeID: clientID.String(), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "https://reg.example"+test.path, nil)
			err := validateFederatedCallbackRoute(req, test.callback, clientID, test.routeID)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFederatedCallbackMisroutedOrigin(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	callback := "https://callback.example/mcp/idp_callback/" + clientID.String()
	federation := &FederatedChallenge{CallbackURL: callback, ClientID: clientID}
	for _, test := range []struct{ name, origin, reason string }{
		{"recorded host", "https://callback.example", ""},
		{"other host", "https://other.example", "origin_mismatch"},
		{"wrong scheme", "http://callback.example", "origin_mismatch"},
		{"wrong port", "https://callback.example:8443", "origin_mismatch"},
		{"missing origin", "", "origin_mismatch"},
		{"invalid origin", "://", "origin_mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, callback, nil)
			require.Equal(t, test.reason, federatedCallbackMisrouted(req, federation, clientID.String(), test.origin))
		})
	}
}
