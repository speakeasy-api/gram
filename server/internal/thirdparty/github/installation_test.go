package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestAPIBase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ input, want string }{
		{"", "https://api.github.com"}, {"https://github.com/", "https://api.github.com"},
		{"https://GITHUB.COM", "https://api.github.com"},
		{"https://github.com:443", "https://api.github.com"},
		{"https://git.example.com", "https://git.example.com/api/v3"},
		{"https://git.example.com:8443/", "https://git.example.com:8443/api/v3"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := APIBase(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	for _, raw := range []string{"http://github.com", "https://user:secret@github.com", "https://github.com/api/v3", "https://github.com?", "https://github.com?q=1", "https://github.com/#frag", "//github.com", "https:///", "https://github.com/%2f"} {
		t.Run(raw, func(t *testing.T) { _, err := APIBase(raw); require.Error(t, err) })
	}
}

func TestInstallationExchange(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	for _, host := range []string{"https://github.com", "https://git.example.com"} {
		t.Run(host, func(t *testing.T) {
			base, err := APIBase(host)
			require.NoError(t, err)
			expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
			calls := 0
			client, err := NewInstanceClient(123, private, host, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "POST", req.Method)
				require.Equal(t, base+"/app/installations/456/access_tokens", req.URL.String())
				require.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
				parsed, err := jwt.Parse(strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "), func(token *jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("123"))
				require.NoError(t, err)
				require.True(t, parsed.Valid)
				return jsonResponse(t, http.StatusCreated, map[string]any{"token": "TEST_INSTALLATION_TOKEN", "expires_at": expires}), nil
			})})
			require.NoError(t, err)
			token, expiry, err := client.MintInstallationToken(t.Context(), 456)
			require.NoError(t, err)
			require.Equal(t, "TEST_INSTALLATION_TOKEN", token)
			require.Equal(t, expires, expiry)
			_, err = client.InstallationToken(t.Context(), 456)
			require.NoError(t, err)
			_, err = client.InstallationToken(t.Context(), 456)
			require.NoError(t, err)
			require.Equal(t, 2, calls, "cached installation token is reused")
		})
	}
	for _, tc := range []struct {
		name   string
		status int
		body   any
	}{
		{"reject", 403, map[string]string{"message": "TEST_SECRET_MUST_NOT_LEAK"}},
		{"redirect", 302, map[string]string{"message": "TEST_SECRET_MUST_NOT_LEAK"}},
		{"empty", 201, map[string]any{"expires_at": time.Now().Add(time.Hour)}},
		{"expired", 201, map[string]any{"token": "TEST_SECRET_MUST_NOT_LEAK", "expires_at": time.Now().Add(-time.Hour)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewInstanceClient(123, private, "https://github.com", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return jsonResponse(t, tc.status, tc.body), nil })})
			require.NoError(t, err)
			_, _, err = client.MintInstallationToken(t.Context(), 456)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "TEST_SECRET_MUST_NOT_LEAK")
		})
	}
}
