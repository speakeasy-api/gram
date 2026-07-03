package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func Test_writeOAuthServerMetadataResponse_Static(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	w := httptest.NewRecorder()

	result := &wellknown.OAuthServerMetadataResult{
		Kind: wellknown.OAuthServerMetadataResultKindStatic,
		Static: &wellknown.OAuthServerMetadata{
			Issuer:                        "https://example.test/oauth/foo",
			AuthorizationEndpoint:         "https://example.test/oauth/foo/authorize",
			TokenEndpoint:                 "https://example.test/oauth/foo/token",
			RegistrationEndpoint:          "https://example.test/oauth/foo/register",
			ScopesSupported:               []string{"offline_access"},
			ResponseTypesSupported:        []string{"code"},
			GrantTypesSupported:           []string{"authorization_code", "refresh_token"},
			CodeChallengeMethodsSupported: []string{"S256"},
		},
		Raw:      nil,
		ProxyURL: "",
	}

	err := writeOAuthServerMetadataResponse(t.Context(), logger, w, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/foo", nil), result)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=60", w.Header().Get("Cache-Control"))
	require.NotEmpty(t, w.Header().Get("ETag"))

	var got wellknown.OAuthServerMetadata
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, *result.Static, got)
}

func Test_writeOAuthServerMetadataResponse_Raw(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	w := httptest.NewRecorder()

	raw := json.RawMessage(`{"issuer":"https://example.test/raw"}`)
	result := &wellknown.OAuthServerMetadataResult{
		Kind:     wellknown.OAuthServerMetadataResultKindRaw,
		Static:   nil,
		Raw:      raw,
		ProxyURL: "",
	}

	err := writeOAuthServerMetadataResponse(t.Context(), logger, w, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/foo", nil), result)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=60", w.Header().Get("Cache-Control"))
	require.NotEmpty(t, w.Header().Get("ETag"))
	require.JSONEq(t, string(raw), w.Body.String())
}

// Test_writeOAuthServerMetadataResponse_UnknownKind_DoesNotWriteResponse is a
// regression test for AGE-1970. The handler used to call WriteHeader(200)
// before constructing the body, so error paths in body construction left the
// caller's middleware unable to emit the real error status (Go silently
// drops a second WriteHeader). Asserting that no headers or body were
// written on the error path catches reintroductions of that pattern.
func Test_writeOAuthServerMetadataResponse_UnknownKind_DoesNotWriteResponse(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	w := httptest.NewRecorder()

	result := &wellknown.OAuthServerMetadataResult{
		Kind:     wellknown.OAuthServerMetadataResultKind("bogus"),
		Static:   nil,
		Raw:      nil,
		ProxyURL: "",
	}

	err := writeOAuthServerMetadataResponse(t.Context(), logger, w, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/foo", nil), result)
	require.Error(t, err)
	require.Empty(t, w.Header().Get("Content-Type"))
	require.Empty(t, w.Body.Bytes())
}

func Test_writeOAuthProtectedResourceMetadataResponse_Success(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	w := httptest.NewRecorder()

	metadata := &wellknown.OAuthProtectedResourceMetadata{
		Resource:               "https://example.test/mcp/foo",
		AuthorizationServers:   []string{"https://example.test/oauth/foo"},
		ScopesSupported:        []string{"offline_access"},
		BearerMethodsSupported: nil,
		ResourceDocumentation:  "",
	}

	err := writeOAuthProtectedResourceMetadataResponse(t.Context(), logger, w, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/foo", nil), metadata)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=60", w.Header().Get("Cache-Control"))
	require.NotEmpty(t, w.Header().Get("ETag"))

	var got wellknown.OAuthProtectedResourceMetadata
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, *metadata, got)
}

// Test_writeOAuthProtectedResourceMetadataResponse_OmitsOptionalFields locks
// down the served wire shape against accidental expansion. Fields beyond the
// minimum set required by existing server-side callers are tagged omitempty
// on [wellknown.OAuthProtectedResourceMetadata]; if any of them stop being
// omitempty, or new always-emitted fields are added, this test fails so the
// change is made consciously.
func Test_writeOAuthProtectedResourceMetadataResponse_OmitsOptionalFields(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	w := httptest.NewRecorder()

	metadata := &wellknown.OAuthProtectedResourceMetadata{
		Resource:               "https://example.test/mcp/foo",
		AuthorizationServers:   []string{"https://example.test/oauth/foo"},
		ScopesSupported:        []string{"offline_access"},
		BearerMethodsSupported: nil,
		ResourceDocumentation:  "",
	}

	err := writeOAuthProtectedResourceMetadataResponse(t.Context(), logger, w, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/foo", nil), metadata)
	require.NoError(t, err)

	const expected = `{
		"resource": "https://example.test/mcp/foo",
		"authorization_servers": ["https://example.test/oauth/foo"],
		"scopes_supported": ["offline_access"]
	}`
	require.JSONEq(t, expected, w.Body.String())
	require.NotContains(t, w.Body.String(), "bearer_methods_supported")
	require.NotContains(t, w.Body.String(), "resource_documentation")
}

// Test_writeOAuthProtectedResourceMetadataResponse_EmitsAllFields asserts that
// every field on [wellknown.OAuthProtectedResourceMetadata] serializes under
// its expected RFC 9728 JSON name when set. Pairs with the OmitsOptionalFields
// test to lock the full wire shape.
func Test_writeOAuthProtectedResourceMetadataResponse_EmitsAllFields(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	w := httptest.NewRecorder()

	metadata := &wellknown.OAuthProtectedResourceMetadata{
		Resource:               "https://example.test/mcp/foo",
		AuthorizationServers:   []string{"https://example.test/oauth/foo"},
		ScopesSupported:        []string{"offline_access", "read"},
		BearerMethodsSupported: []string{"header"},
		ResourceDocumentation:  "https://docs.example.test",
	}

	err := writeOAuthProtectedResourceMetadataResponse(t.Context(), logger, w, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/foo", nil), metadata)
	require.NoError(t, err)

	const expected = `{
		"resource": "https://example.test/mcp/foo",
		"authorization_servers": ["https://example.test/oauth/foo"],
		"scopes_supported": ["offline_access", "read"],
		"bearer_methods_supported": ["header"],
		"resource_documentation": "https://docs.example.test"
	}`
	require.JSONEq(t, expected, w.Body.String())
}
