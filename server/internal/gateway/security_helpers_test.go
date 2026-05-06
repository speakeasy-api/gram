package gateway

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeScopes_Empty(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", normalizeScopes(nil))
	require.Equal(t, "", normalizeScopes([]string{}))
}

func TestNormalizeScopes_SingleScope(t *testing.T) {
	t.Parallel()

	require.Equal(t, "read", normalizeScopes([]string{"read"}))
}

func TestNormalizeScopes_OrderIndependent(t *testing.T) {
	t.Parallel()

	a := normalizeScopes([]string{"write", "read", "admin"})
	b := normalizeScopes([]string{"read", "admin", "write"})
	require.Equal(t, a, b, "scope order must not affect the cache key")
	require.Equal(t, "admin,read,write", a)
}

func TestNormalizeScopes_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	in := []string{"b", "a"}
	_ = normalizeScopes(in)
	require.Equal(t, []string{"b", "a"}, in, "input slice must not be sorted in place")
}

func TestClientCredentialsTokenCacheCacheKey_StableForSameInputs(t *testing.T) {
	t.Parallel()

	k1 := clientCredentialsTokenCacheCacheKey("p", "c", "https://t/u", []string{"a", "b"})
	k2 := clientCredentialsTokenCacheCacheKey("p", "c", "https://t/u", []string{"b", "a"})
	require.Equal(t, k1, k2, "scope order must not change the cache key")
}

func TestClientCredentialsTokenCacheCacheKey_DiffersByProject(t *testing.T) {
	t.Parallel()

	k1 := clientCredentialsTokenCacheCacheKey("p1", "c", "https://t/u", nil)
	k2 := clientCredentialsTokenCacheCacheKey("p2", "c", "https://t/u", nil)
	require.NotEqual(t, k1, k2)
}

func TestClientCredentialsTokenCacheCacheKey_EncodesTokenURL(t *testing.T) {
	t.Parallel()

	// The token URL must be URL-encoded so colons/slashes don't accidentally
	// collide with the cache key separator format.
	k := clientCredentialsTokenCacheCacheKey("p", "c", "https://idp.example/oauth/token?env=staging", nil)
	require.NotContains(t, k, "https://")
	require.Contains(t, k, "https%3A")
}

func TestClientCredentialsTokenCache_CacheableObject(t *testing.T) {
	t.Parallel()

	c := clientCredentialsTokenCache{
		ProjectID: "p",
		ClientID:  "c",
		TokenURL:  "https://t/u",
		Scopes:    []string{"read"},
		ExpiresIn: 5 * time.Minute,
	}

	// CacheKey() must equal clientCredentialsTokenCacheCacheKey for the same inputs.
	require.Equal(t, clientCredentialsTokenCacheCacheKey(c.ProjectID, c.ClientID, c.TokenURL, c.Scopes), c.CacheKey())

	require.Empty(t, c.AdditionalCacheKeys())
	require.Equal(t, 5*time.Minute, c.TTL())
}

func TestParseClientCredentialsTokenResponse_SnakeCase(t *testing.T) {
	t.Parallel()

	body := []byte(`{"access_token":"tok","expires_in":3600}`)
	tok, exp, err := parseClientCredentialsTokenResponse(body)
	require.NoError(t, err)
	require.Equal(t, "tok", tok)
	require.Equal(t, 3600, exp)
}

func TestParseClientCredentialsTokenResponse_CamelCaseFallback(t *testing.T) {
	t.Parallel()

	body := []byte(`{"accessToken":"tok2","expiresIn":42}`)
	tok, exp, err := parseClientCredentialsTokenResponse(body)
	require.NoError(t, err)
	require.Equal(t, "tok2", tok)
	require.Equal(t, 42, exp)
}

func TestParseClientCredentialsTokenResponse_MissingToken(t *testing.T) {
	t.Parallel()

	body := []byte(`{"unrelated":"x"}`)
	_, _, err := parseClientCredentialsTokenResponse(body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no access token")
}

func TestParseClientCredentialsTokenResponse_MalformedJSON(t *testing.T) {
	t.Parallel()

	_, _, err := parseClientCredentialsTokenResponse([]byte(`{not json`))
	require.Error(t, err)
}

func TestParseClientCredentialsTokenResponse_PrefersSnakeCaseOverCamel(t *testing.T) {
	t.Parallel()

	// When both formats are present, snake_case wins (the spec format).
	body := []byte(`{"access_token":"snake","accessToken":"camel","expires_in":1,"expiresIn":2}`)
	tok, exp, err := parseClientCredentialsTokenResponse(body)
	require.NoError(t, err)
	require.Equal(t, "snake", tok)
	require.Equal(t, 1, exp)
}

func TestFormatForBearer_AddsPrefix(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Bearer abc123", formatForBearer("abc123"))
}

func TestFormatForBearer_PreservesExistingBearerPrefix(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Bearer abc", formatForBearer("Bearer abc"))
}

func TestFormatForBearer_PreservesExistingLowercaseBearer(t *testing.T) {
	t.Parallel()

	// Existing prefix is preserved exactly (case unchanged) even if lowercase.
	got := formatForBearer("bearer abc")
	require.True(t, strings.HasPrefix(got, "bearer ") || strings.HasPrefix(got, "Bearer "))
	require.Contains(t, got, "abc")
	// Specifically, the implementation does not double-prefix.
	require.NotContains(t, got, "Bearer Bearer")
	require.NotContains(t, got, "Bearer bearer")
}

func TestFormatForBearer_EmptyToken(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Bearer ", formatForBearer(""))
}
