package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
)

func Test_writeOAuthServerMetadataResponse_Static(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
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

	err := writeOAuthServerMetadataResponse(t.Context(), logger, w, result)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var got wellknown.OAuthServerMetadata
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, *result.Static, got)
}

func Test_writeOAuthServerMetadataResponse_Raw(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	w := httptest.NewRecorder()

	raw := json.RawMessage(`{"issuer":"https://example.test/raw"}`)
	result := &wellknown.OAuthServerMetadataResult{
		Kind:     wellknown.OAuthServerMetadataResultKindRaw,
		Static:   nil,
		Raw:      raw,
		ProxyURL: "",
	}

	err := writeOAuthServerMetadataResponse(t.Context(), logger, w, result)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
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

	logger := slog.New(slog.DiscardHandler)
	w := httptest.NewRecorder()

	result := &wellknown.OAuthServerMetadataResult{
		Kind:     wellknown.OAuthServerMetadataResultKind("bogus"),
		Static:   nil,
		Raw:      nil,
		ProxyURL: "",
	}

	err := writeOAuthServerMetadataResponse(t.Context(), logger, w, result)
	require.Error(t, err)
	require.Empty(t, w.Header().Get("Content-Type"))
	require.Empty(t, w.Body.Bytes())
}

func Test_writeOAuthProtectedResourceMetadataResponse_Success(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	w := httptest.NewRecorder()

	metadata := &wellknown.OAuthProtectedResourceMetadata{
		Resource:             "https://example.test/mcp/foo",
		AuthorizationServers: []string{"https://example.test/oauth/foo"},
		ScopesSupported:      []string{"offline_access"},
	}

	err := writeOAuthProtectedResourceMetadataResponse(t.Context(), logger, w, metadata)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var got wellknown.OAuthProtectedResourceMetadata
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, *metadata, got)
}
