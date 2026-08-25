package jwks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRemoteSource_Accepted(t *testing.T) {
	t.Parallel()

	source, err := NewRemoteSource("https://chatgpt.com/oauth/jwks.json")
	require.NoError(t, err)
	require.Equal(t, "https://chatgpt.com/oauth/jwks.json", source.CacheKey())
}

func TestNewRemoteSource_NoOriginRule(t *testing.T) {
	t.Parallel()

	// Deliberately no origin relationship is required between a jwks_uri and
	// whoever published it: RFC 7591 and RFC 8414 constrain the URL only to
	// the https scheme, and real deployments cross hosts (Google publishes
	// issuer accounts.google.com with a jwks_uri on www.googleapis.com).
	source, err := NewRemoteSource("https://www.googleapis.com/oauth2/v3/certs")
	require.NoError(t, err)
	require.Equal(t, "https://www.googleapis.com/oauth2/v3/certs", source.CacheKey())
}

// NewRemoteSource and ValidateURI are checked against one list, so the
// registration-time answer and the resolve-time answer cannot drift apart: a
// jwks_uri that passes registration must not fail to resolve later for a
// reason registration could have caught.
func TestNewRemoteSource_InvalidURIRejected(t *testing.T) {
	t.Parallel()

	for _, uri := range []string{
		"",
		"http://example.com/jwks.json",
		"https://example.com/jwks.json#frag",
		"https://user:pass@example.com/jwks.json",
		"https://user@example.com/jwks.json",
		"https://",
		"https://:443/jwks.json",
		"https://example.com/" + strings.Repeat("a", maxJWKSURILength),
	} {
		_, err := NewRemoteSource(uri)
		require.Error(t, err, "jwks_uri %q should be rejected", uri)
		require.Error(t, ValidateURI(uri), "ValidateURI must agree that %q is rejected", uri)
	}

	for _, uri := range []string{
		"https://chatgpt.com/oauth/jwks.json",
		"https://example.com/.well-known/jwks.json?v=2",
		"https://example.com:8443/jwks.json",
	} {
		_, err := NewRemoteSource(uri)
		require.NoError(t, err, "jwks_uri %q should be accepted", uri)
		require.NoError(t, ValidateURI(uri), "ValidateURI must agree that %q is accepted", uri)
	}
}

func TestNewInlineSource_EmptyRejected(t *testing.T) {
	t.Parallel()

	_, err := NewInlineSource(nil)
	require.Error(t, err)

	_, err = NewInlineSource(json.RawMessage{})
	require.Error(t, err)
}

func TestNewInlineSource_HasNoCacheKey(t *testing.T) {
	t.Parallel()

	source, err := NewInlineSource(json.RawMessage(`{"keys":[]}`))
	require.NoError(t, err)
	require.Empty(t, source.CacheKey())
}
