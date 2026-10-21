package oauthwire_test

import (
	"net/http"
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
		{name: "other scheme", authorization: []string{"Bearer token"}, status: http.StatusBadRequest},
		{name: "similar scheme", authorization: []string{"BasicOther credentials"}, status: http.StatusBadRequest},
		{name: "basic", authorization: []string{"Basic Y2xpZW50OnNlY3JldA=="}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "case insensitive", authorization: []string{"bAsIc Y2xpZW50OnNlY3JldA=="}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "malformed credentials", authorization: []string{"Basic not-base64"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "missing credentials", authorization: []string{"Basic"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "whitespace", authorization: []string{" Basic\t "}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		{name: "basic in later field", authorization: []string{"Bearer token", "Basic invalid"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
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
