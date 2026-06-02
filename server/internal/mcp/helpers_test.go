package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestIsMCPPassthrough(t *testing.T) {
	t.Parallel()

	t.Run("returns_false_for_nil_meta", func(t *testing.T) {
		t.Parallel()
		require.False(t, isMCPPassthrough(nil))
	})

	t.Run("returns_false_for_empty_meta", func(t *testing.T) {
		t.Parallel()
		require.False(t, isMCPPassthrough(map[string]any{}))
	})

	t.Run("returns_false_for_meta_without_gram_kind", func(t *testing.T) {
		t.Parallel()
		meta := map[string]any{
			"other-key": "some-value",
		}
		require.False(t, isMCPPassthrough(meta))
	})

	t.Run("returns_false_for_meta_with_different_kind", func(t *testing.T) {
		t.Parallel()
		meta := map[string]any{
			MetaGramKind: "other-kind",
		}
		require.False(t, isMCPPassthrough(meta))
	})

	t.Run("returns_true_for_meta_with_mcp_passthrough_kind", func(t *testing.T) {
		t.Parallel()
		meta := map[string]any{
			MetaGramKind: "mcp-passthrough",
		}
		require.True(t, isMCPPassthrough(meta))
	})

	t.Run("returns_false_when_kind_is_not_string", func(t *testing.T) {
		t.Parallel()
		meta := map[string]any{
			MetaGramKind: 123, // not a string
		}
		require.False(t, isMCPPassthrough(meta))
	})
}

func TestParseMcpEnvVariables(t *testing.T) {
	t.Parallel()

	t.Run("returns_empty_map_for_no_mcp_headers", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer token")

		result := parseMcpEnvVariables(req, nil)
		require.Empty(t, result)
	})

	t.Run("parses_mcp_prefixed_headers", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("MCP-API-Key", "secret-key")
		req.Header.Set("MCP-User-Id", "12345")

		result := parseMcpEnvVariables(req, nil)
		require.Equal(t, "secret-key", result["api_key"])
		require.Equal(t, "12345", result["user_id"])
	})

	t.Run("ignores_mcp_session_id_header", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("MCP-Session-ID", "session-123")
		req.Header.Set("MCP-Other-Key", "value")

		result := parseMcpEnvVariables(req, nil)
		require.NotContains(t, result, "session_id")
		require.Equal(t, "value", result["other_key"])
	})

	t.Run("maps_display_names_to_actual_header_names", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("MCP-API-Key", "secret-key")

		headerDisplayNames := map[string]string{
			"X-RapidAPI-Key": "API Key",
		}

		result := parseMcpEnvVariables(req, headerDisplayNames)
		// The "api_key" from header should be mapped to "x_rapidapi_key"
		require.Equal(t, "secret-key", result["x_rapidapi_key"])
	})

	t.Run("handles_spaces_in_display_names", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("MCP-My-Secret-Token", "token-value")

		headerDisplayNames := map[string]string{
			"X-Custom-Header": "My Secret Token",
		}

		result := parseMcpEnvVariables(req, headerDisplayNames)
		require.Equal(t, "token-value", result["x_custom_header"])
	})

	t.Run("handles_case_insensitive_header_matching", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("mcp-api-key", "lowercase-key")

		result := parseMcpEnvVariables(req, nil)
		require.Equal(t, "lowercase-key", result["api_key"])
	})
}

func TestIsBinaryMimeType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := testenv.NewLogger(t)

	t.Run("returns_true_for_image_types", func(t *testing.T) {
		t.Parallel()
		require.True(t, isBinaryMimeType(ctx, logger, "image/png"))
		require.True(t, isBinaryMimeType(ctx, logger, "image/jpeg"))
		require.True(t, isBinaryMimeType(ctx, logger, "image/gif"))
	})

	t.Run("returns_true_for_application_octet_stream", func(t *testing.T) {
		t.Parallel()
		require.True(t, isBinaryMimeType(ctx, logger, "application/octet-stream"))
	})

	t.Run("returns_true_for_application_pdf", func(t *testing.T) {
		t.Parallel()
		require.True(t, isBinaryMimeType(ctx, logger, "application/pdf"))
	})

	t.Run("returns_false_for_text_types", func(t *testing.T) {
		t.Parallel()
		require.False(t, isBinaryMimeType(ctx, logger, "text/plain"))
		require.False(t, isBinaryMimeType(ctx, logger, "text/html"))
	})

	t.Run("returns_false_for_json", func(t *testing.T) {
		t.Parallel()
		require.False(t, isBinaryMimeType(ctx, logger, "application/json"))
	})

	t.Run("returns_false_for_invalid_mime_type", func(t *testing.T) {
		t.Parallel()
		require.False(t, isBinaryMimeType(ctx, logger, "invalid-mime"))
	})

	t.Run("returns_false_for_text_suffix_patterns", func(t *testing.T) {
		t.Parallel()
		require.False(t, isBinaryMimeType(ctx, logger, "application/vnd.api+json"))
		require.False(t, isBinaryMimeType(ctx, logger, "application/hal+xml"))
	})
}

func TestResult_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("marshals_result_with_jsonrpc_version", func(t *testing.T) {
		t.Parallel()
		r := result[string]{
			ID:     mcpjsonrpc.NumberID(1),
			Result: "test-result",
		}
		data, err := json.Marshal(r)
		require.NoError(t, err)

		var parsed map[string]any
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		require.Equal(t, "2.0", parsed["jsonrpc"])
		require.Equal(t, "test-result", parsed["result"])
	})
}

func TestResult_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("unmarshals_valid_result", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"jsonrpc": "2.0", "id": 1, "result": "test-value"}`)
		var r result[string]
		err := json.Unmarshal(data, &r)
		require.NoError(t, err)
		require.Equal(t, "test-value", r.Result)
	})

	t.Run("returns_error_for_wrong_jsonrpc_version", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"jsonrpc": "1.0", "id": 1, "result": "test-value"}`)
		var r result[string]
		err := json.Unmarshal(data, &r)
		require.Error(t, err)
		require.Contains(t, err.Error(), "1.0")
	})

	t.Run("returns_error_for_invalid_json", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{invalid}`)
		var r result[string]
		err := json.Unmarshal(data, &r)
		require.Error(t, err)
	})
}
