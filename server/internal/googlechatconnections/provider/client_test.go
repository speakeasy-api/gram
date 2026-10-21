package provider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/stretchr/testify/require"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}
}
func TestOAuthUsesPKCEAndOfflineConsent(t *testing.T) {
	t.Parallel()
	c := &Client{Config: Config{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://example.test/callback"}, HTTP: nil}
	url := c.AuthorizationURL("state", "challenge")
	for _, part := range []string{"access_type=offline", "prompt=consent", "code_challenge=challenge", "code_challenge_method=S256", "state=state"} {
		require.Contains(t, url, part)
	}
}
func TestExchangeRequiresVerifiedGoogleAccount(t *testing.T) {
	t.Parallel()
	for _, verified := range []bool{true, false} {
		t.Run(map[bool]string{true: "verified", false: "unverified"}[verified], func(t *testing.T) {
			t.Parallel()
			calls := 0
			c := &Client{Config: Config{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://example.test/callback"}, HTTP: &guardian.HTTPClient{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host == "oauth2.googleapis.com" {
					require.NoError(t, r.ParseForm())
					require.Equal(t, "verifier", r.Form.Get("code_verifier"))
					return fakeResponse(200, `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","scope":"https://www.googleapis.com/auth/chat.spaces.create https://www.googleapis.com/auth/chat.spaces.readonly https://www.googleapis.com/auth/chat.memberships https://www.googleapis.com/auth/chat.memberships.app"}`), nil
				}
				require.Equal(t, "openidconnect.googleapis.com", r.URL.Host)
				body := `{"sub":"123","email":"person@example.test","email_verified":false}`
				if verified {
					body = strings.Replace(body, "false", "true", 1)
				}
				return fakeResponse(200, body), nil
			})}}
			token, identity, err := c.Exchange(t.Context(), "code", "verifier")
			if verified {
				require.NoError(t, err)
				require.Equal(t, "123", identity.Subject)
				require.Equal(t, "refresh", token.RefreshToken)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, 2, calls)
		})
	}
}
func TestCallRejectsEndpointsAndSanitizesProviderErrors(t *testing.T) {
	t.Parallel()
	calls := 0
	c := &Client{HTTP: &guardian.HTTPClient{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return fakeResponse(403, `{"error":"secret refresh credentials"}`), nil
	})}}
	err := c.Call(t.Context(), "token", http.MethodGet, "https://attacker.test/steal", nil, nil)
	require.Error(t, err)
	require.Zero(t, calls)
	err = c.Call(t.Context(), "token", http.MethodGet, "https://chat.googleapis.com/v1/spaces", nil, nil)
	require.ErrorContains(t, err, "403")
	require.NotContains(t, err.Error(), "secret")
	require.Equal(t, 1, calls)
}
func TestCallDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	calls := 0
	c := &Client{HTTP: &guardian.HTTPClient{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		r := fakeResponse(302, "")
		r.Header.Set("Location", "https://attacker.test/")
		return r, nil
	})}}
	require.Error(t, c.Call(context.Background(), "token", http.MethodGet, "https://chat.googleapis.com/v1/spaces", nil, nil))
	require.Equal(t, 1, calls)
}

func TestSubscriptionCreatesResourcePayloadAndPolls(t *testing.T) {
	t.Parallel()
	calls := 0
	c := &Client{Config: Config{Topic: "projects/test/topics/chat"}, HTTP: &guardian.HTTPClient{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			require.Equal(t, http.MethodPost, r.Method)
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			options, ok := body["payloadOptions"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, true, options["includeResource"])
			require.NotContains(t, options, "includeResourceData")
			return fakeResponse(200, `{"name":"operations/create-test","done":false}`), nil
		}
		require.Equal(t, "/v1/operations/create-test", r.URL.Path)
		return fakeResponse(200, `{"done":true,"response":{"name":"subscriptions/test","targetResource":"//chat.googleapis.com/spaces/test"}}`), nil
	})}}
	sub, err := c.CreateSubscription(t.Context(), "token", "spaces/test")
	require.NoError(t, err)
	require.Equal(t, "subscriptions/test", sub.Name)
	require.Equal(t, 2, calls)
}

func TestUserConsentDoesNotRequestMessageHistory(t *testing.T) {
	t.Parallel()
	c := &Client{}
	consent, err := url.Parse(c.AuthorizationURL("state", "challenge"))
	require.NoError(t, err)
	granted := strings.Fields(consent.Query().Get("scope"))
	require.NotContains(t, granted, "https://www.googleapis.com/auth/chat.messages.readonly")
	require.NotContains(t, granted, "https://www.googleapis.com/auth/chat.spaces")
	require.Contains(t, granted, "https://www.googleapis.com/auth/chat.spaces.create")
}

func TestPushSubscriptionRecreatesImmutableSpaceFilter(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]string{"type": "service_account", "client_email": "chat@example.iam.gserviceaccount.com", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))})
	require.NoError(t, err)
	var calls []string
	c := &Client{Config: Config{ServiceAccountJSON: string(raw), Topic: "projects/test/topics/chat", PushServiceAccount: "push@example.iam.gserviceaccount.com"}, HTTP: &guardian.HTTPClient{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "oauth2.googleapis.com" {
			return fakeResponse(200, `{"access_token":"app-token","token_type":"Bearer","expires_in":3600}`), nil
		}
		require.Equal(t, "pubsub.googleapis.com", r.URL.Host)
		calls = append(calls, r.Method)
		switch len(calls) {
		case 1:
			return fakeResponse(http.StatusConflict, `{}`), nil
		case 2:
			return fakeResponse(200, `{"topic":"projects/test/topics/chat","filter":""}`), nil
		case 3:
			return fakeResponse(http.StatusOK, `{}`), nil
		default:
			var body struct {
				Filter string `json:"filter"`
				Topic  string `json:"topic"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, `attributes.ce-subject = "//chat.googleapis.com/spaces/test"`, body.Filter)
			require.Equal(t, "projects/test/topics/chat", body.Topic)
			return fakeResponse(200, `{}`), nil
		}
	})}}
	require.NoError(t, c.UpsertPushSubscription(t.Context(), "projects/test/subscriptions/chat-test", "https://app.example.test/webhook", "https://app.example.test/audience", "spaces/test"))
	require.Equal(t, []string{"PUT", "GET", "DELETE", "PUT"}, calls)
}
