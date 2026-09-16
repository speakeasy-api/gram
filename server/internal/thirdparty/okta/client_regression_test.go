package okta

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type reviewTransport func(*http.Request) (*http.Response, error)

func (f reviewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reviewResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestClient_DelayedUnauthorizedPreservesRefreshedToken(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	listApps(t, tc)
	old, ok := tc.client.cachedToken()
	require.True(t, ok)
	transport := tc.client.httpClient.Transport
	bEntered, releaseB := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseB) }) }
	defer release()
	tc.client.httpClient.Transport = reviewTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "DPoP "+old.accessToken {
			return transport.RoundTrip(r)
		}
		if r.URL.Query().Get("q") == "B" {
			close(bEntered)
			select {
			case <-releaseB:
			case <-r.Context().Done():
				return nil, fmt.Errorf("held response: %w", r.Context().Err())
			}
		} else {
			select {
			case <-bEntered:
			case <-r.Context().Done():
				return nil, fmt.Errorf("wait for B: %w", r.Context().Err())
			}
		}
		return reviewResponse(http.StatusUnauthorized, `{}`), nil
	})
	aDone, bDone := make(chan error, 1), make(chan error, 1)
	for _, request := range []struct {
		query string
		done  chan error
	}{{"A", aDone}, {"B", bDone}} {
		go func() {
			_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: request.query, Status: "", Limit: 0})
			request.done <- err
		}()
	}
	select {
	case err := <-aDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("A did not refresh")
	}
	refreshed, ok := tc.client.cachedToken()
	require.True(t, ok)
	require.NotEqual(t, old.accessToken, refreshed.accessToken)
	release()
	select {
	case err := <-bDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("B did not retry")
	}
	final, ok := tc.client.cachedToken()
	require.True(t, ok)
	require.Equal(t, refreshed.accessToken, final.accessToken)
	require.Equal(t, 2, tc.stub.counts().issuedTokens)
}

func TestClient_MintCarriesNonceFromRateLimitResponse(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	attempts := 0
	tc.client.httpClient.Transport = reviewTransport(func(r *http.Request) (*http.Response, error) {
		attempts++
		parts := strings.Split(r.Header.Get("DPoP"), ".")
		require.Len(t, parts, 3)
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		require.NoError(t, err)
		var claims struct {
			Nonce string `json:"nonce"`
		}
		require.NoError(t, json.Unmarshal(payload, &claims))
		switch attempts {
		case 1:
			require.Empty(t, claims.Nonce)
			response := reviewResponse(http.StatusBadRequest, `{"error":"use_dpop_nonce"}`)
			response.Header.Set("DPoP-Nonce", "N1")
			return response, nil
		case 2:
			require.Equal(t, "N1", claims.Nonce)
			response := reviewResponse(http.StatusTooManyRequests, `{}`)
			response.Header.Set("DPoP-Nonce", "N2")
			return response, nil
		default:
			require.Equal(t, "N2", claims.Nonce)
			return reviewResponse(http.StatusOK, `{"access_token":"test-token","token_type":"DPoP","expires_in":3600,"scope":"okta.apps.read"}`), nil
		}
	})
	tok, err := tc.client.token(t.Context())
	require.NoError(t, err)
	require.Equal(t, "test-token", tok.accessToken)
	require.Equal(t, 3, attempts)
}

func TestClient_VerifyScopes_OAuthErrors(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"invalid_scope", "consent_required", "invalid_client", "invalid_grant", "consent_required_extra"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			tc := newDefaultTestClient(t)
			listApps(t, tc)
			tc.client.httpClient.Transport = reviewTransport(func(_ *http.Request) (*http.Response, error) {
				return reviewResponse(http.StatusBadRequest, fmt.Sprintf(`{"error":%q}`, code)), nil
			})
			required := []string{"okta.users.read"}
			result, err := tc.client.VerifyScopes(t.Context(), required)
			if code == "invalid_scope" || code == "consent_required" {
				require.NoError(t, err)
				require.Empty(t, result.Granted)
				require.Equal(t, required, result.Missing)
				require.False(t, result.DPoPBound)
				require.True(t, result.ExpiresAt.IsZero())
			} else {
				var apiErr *APIError
				require.ErrorAs(t, err, &apiErr)
				require.Equal(t, code, apiErr.ErrorCode)
				require.Nil(t, result)
			}
			_, cached := tc.client.cachedToken()
			require.False(t, cached)
		})
	}
}

func TestClient_APIErrorPaths(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"list", "get", "token"} {
		for _, trailingSlash := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/trailing=%t", operation, trailingSlash), func(t *testing.T) {
				t.Parallel()
				tc := newDefaultTestClient(t)
				if trailingSlash {
					tc.client.orgURL.Path = "/"
				}
				transport := tc.client.httpClient.Transport
				tc.client.httpClient.Transport = reviewTransport(func(r *http.Request) (*http.Response, error) {
					if operation != "token" && r.URL.Path == tokenEndpointPath {
						return transport.RoundTrip(r)
					}
					require.NotContains(t, r.URL.EscapedPath(), "//")
					return reviewResponse(http.StatusForbidden, `{}`), nil
				})
				var err error
				expected := "/api/v1/apps"
				switch operation {
				case "list":
					_, err = tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
				case "get":
					_, err = tc.client.GetApp(t.Context(), "test-app")
					expected += "/test-app"
				case "token":
					_, err = tc.client.token(t.Context())
					expected = tokenEndpointPath
				}
				var apiErr *APIError
				require.ErrorAs(t, err, &apiErr)
				require.Equal(t, expected, apiErr.Path)
				require.NotContains(t, apiErr.Path, "//")
			})
		}
	}
}
