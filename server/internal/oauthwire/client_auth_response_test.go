package oauthwire_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

func TestInvalidClientResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		authorization []string
		status        int
		challenge     string
	}{
		{name: "no header", status: http.StatusBadRequest},
		{name: "empty header", authorization: []string{""}, status: http.StatusBadRequest},
		{name: "other scheme", authorization: []string{"Bearer token"}, status: http.StatusUnauthorized, challenge: `Bearer realm="Speakeasy"`},
		{name: "similar scheme", authorization: []string{"BasicOther credentials"}, status: http.StatusUnauthorized, challenge: `BasicOther realm="Speakeasy"`},
		{name: "basic", authorization: []string{"Basic Y2xpZW50OnNlY3JldA=="}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "case insensitive", authorization: []string{"bAsIc Y2xpZW50OnNlY3JldA=="}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "malformed credentials", authorization: []string{"Basic not-base64"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "missing credentials", authorization: []string{"Basic"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "whitespace", authorization: []string{" Basic\t "}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "multiple fields", authorization: []string{"Bearer token", "Basic invalid"}, status: http.StatusBadRequest},
		{name: "invalid scheme", authorization: []string{`Bad"Scheme token`}, status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status, challenge := oauthwire.InvalidClientResponse(http.Header{"Authorization": tt.authorization})
			require.Equal(t, tt.status, status)
			require.Equal(t, tt.challenge, challenge)
		})
	}
}

func TestClientAuthenticationError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		headers []string
		form    url.Values
		code    string
	}{
		{name: "public", form: url.Values{"client_id": {"client"}}},
		{name: "body secret", form: url.Values{"client_id": {"client"}, "client_secret": {"secret"}}},
		{name: "assertion", form: url.Values{"client_assertion": {"assertion"}, "client_assertion_type": {"jwt"}}},
		{name: "basic", headers: []string{"Basic Y2xpZW50OnNlY3JldA=="}},
		{name: "basic with identifier", headers: []string{"Basic Y2xpZW50OnNlY3JldA=="}, form: url.Values{"client_id": {"client"}}},
		{name: "basic with empty secret", headers: []string{"Basic Y2xpZW50OnNlY3JldA=="}, form: url.Values{"client_secret": {""}}},
		{name: "conflicting identifier", headers: []string{"Basic Y2xpZW50OnNlY3JldA=="}, form: url.Values{"client_id": {"other"}}, code: "invalid_request"},
		{name: "duplicate header", headers: []string{"Basic Y2xpZW50OnNlY3JldA==", "Basic Y2xpZW50OnNlY3JldA=="}, code: "invalid_request"},
		{name: "duplicate id", form: url.Values{"client_id": {"client", "client"}}, code: "invalid_request"},
		{name: "duplicate secret", form: url.Values{"client_secret": {"secret", "secret"}}, code: "invalid_request"},
		{name: "duplicate assertion", form: url.Values{"client_assertion": {"assertion", "assertion"}}, code: "invalid_request"},
		{name: "duplicate assertion type", form: url.Values{"client_assertion_type": {"jwt", "jwt"}}, code: "invalid_request"},
		{name: "header and body secret", headers: []string{"Basic Y2xpZW50OnNlY3JldA=="}, form: url.Values{"client_secret": {"secret"}}, code: "invalid_request"},
		{name: "header and assertion", headers: []string{"Basic Y2xpZW50OnNlY3JldA=="}, form: url.Values{"client_assertion": {"assertion"}}, code: "invalid_request"},
		{name: "secret and assertion", form: url.Values{"client_secret": {"secret"}, "client_assertion": {"assertion"}}, code: "invalid_request"},
		{name: "unsupported scheme", headers: []string{"Bearer token"}, code: "invalid_client"},
		{name: "unsupported scheme with body secret", headers: []string{"Bearer token"}, form: url.Values{"client_secret": {"secret"}}, code: "invalid_request"},
		{name: "unsupported scheme with identifier", headers: []string{"Bearer token"}, form: url.Values{"client_id": {"client"}}, code: "invalid_client"},
		{name: "malformed basic with identifier", headers: []string{"Basic invalid"}, form: url.Values{"client_id": {"client"}}, code: "invalid_client"},
		{name: "empty basic id", headers: []string{"Basic OnNlY3JldA=="}, code: "invalid_client"},
		{name: "malformed scheme", headers: []string{`Bad"Scheme token`}, code: "invalid_request"},
		{name: "empty header", headers: []string{""}, code: "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(tc.form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for _, value := range tc.headers {
				r.Header.Add("Authorization", value)
			}
			require.NoError(t, r.ParseForm())
			require.Equal(t, tc.code, oauthwire.ClientAuthenticationError(r))
		})
	}
}
