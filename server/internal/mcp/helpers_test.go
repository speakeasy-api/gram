package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestMsgID_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("marshals_int64_id", func(t *testing.T) {
		t.Parallel()
		id := msgID{format: 1, Number: 42}
		result, err := json.Marshal(id)
		require.NoError(t, err)
		require.Equal(t, "42", string(result))
	})

	t.Run("marshals_string_id", func(t *testing.T) {
		t.Parallel()
		id := msgID{format: 2, String: "test-id"}
		result, err := json.Marshal(id)
		require.NoError(t, err)
		require.Equal(t, `"test-id"`, string(result))
	})
}

func TestMsgID_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("unmarshals_int64_id", func(t *testing.T) {
		t.Parallel()
		var id msgID
		err := json.Unmarshal([]byte("42"), &id)
		require.NoError(t, err)
		require.Equal(t, byte(1), id.format)
		require.Equal(t, int64(42), id.Number)
	})

	t.Run("unmarshals_string_id", func(t *testing.T) {
		t.Parallel()
		var id msgID
		err := json.Unmarshal([]byte(`"test-id"`), &id)
		require.NoError(t, err)
		require.Equal(t, byte(2), id.format)
		require.Equal(t, "test-id", id.String)
	})

	t.Run("returns_error_for_invalid_json", func(t *testing.T) {
		t.Parallel()
		var id msgID
		err := json.Unmarshal([]byte(`{}`), &id)
		require.Error(t, err)
	})
}

func TestMsgID_Value(t *testing.T) {
	t.Parallel()

	t.Run("returns_int64_as_string", func(t *testing.T) {
		t.Parallel()
		id := msgID{format: 1, Number: 123}
		require.Equal(t, "123", id.Value())
	})

	t.Run("returns_string_value", func(t *testing.T) {
		t.Parallel()
		id := msgID{format: 2, String: "my-id"}
		require.Equal(t, "my-id", id.Value())
	})
}

func TestErrorCode_String(t *testing.T) {
	t.Parallel()

	t.Run("returns_string_representation", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "-32601", methodNotFound.String())
		require.Equal(t, "-32700", parseError.String())
		require.Equal(t, "-32600", invalidRequest.String())
	})
}

func TestErrorCode_UserMessage(t *testing.T) {
	t.Parallel()

	t.Run("returns_user_message_for_known_codes", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "method not allowed", methodNotAllowed.UserMessage())
		require.Equal(t, "invalid json was received by the server", parseError.UserMessage())
		require.Equal(t, "json sent is not a valid request object", invalidRequest.UserMessage())
		require.Equal(t, "method does not exist or is not available", methodNotFound.UserMessage())
		require.Equal(t, "invalid method parameters", invalidParams.UserMessage())
		require.Equal(t, "internal json-rpc error", internalError.UserMessage())
	})

	t.Run("returns_default_message_for_unknown_code", func(t *testing.T) {
		t.Parallel()
		unknownCode := errorCode(-99999)
		require.Equal(t, "an unexpected error occurred", unknownCode.UserMessage())
	})
}

func TestRpcError_Error(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_string", func(t *testing.T) {
		t.Parallel()
		err := &rpcError{
			Code:    methodNotFound,
			Message: "test error",
		}
		require.Contains(t, err.Error(), "test error")
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

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
			ID:     msgID{format: 1, Number: 1},
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

func TestBatchedRawRequest_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("unmarshals_array_of_requests", func(t *testing.T) {
		t.Parallel()
		data := []byte(`[{"jsonrpc": "2.0", "id": 1, "method": "test1"}, {"jsonrpc": "2.0", "id": 2, "method": "test2"}]`)
		var batch batchedRawRequest
		err := json.Unmarshal(data, &batch)
		require.NoError(t, err)
		require.Len(t, batch, 2)
		require.Equal(t, "test1", batch[0].Method)
		require.Equal(t, "test2", batch[1].Method)
	})

	t.Run("unmarshals_single_request_as_batch_of_one", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"jsonrpc": "2.0", "id": 1, "method": "single"}`)
		var batch batchedRawRequest
		err := json.Unmarshal(data, &batch)
		require.NoError(t, err)
		require.Len(t, batch, 1)
		require.Equal(t, "single", batch[0].Method)
	})

	t.Run("returns_error_for_invalid_json", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{invalid}`)
		var batch batchedRawRequest
		err := json.Unmarshal(data, &batch)
		require.Error(t, err)
	})
}

func TestRpcError_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("marshals_error_with_jsonrpc_version", func(t *testing.T) {
		t.Parallel()
		rpcErr := &rpcError{
			ID:      msgID{format: 1, Number: 1},
			Code:    methodNotFound,
			Message: "test error message",
		}
		data, err := json.Marshal(rpcErr)
		require.NoError(t, err)

		var parsed map[string]any
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		require.Equal(t, "2.0", parsed["jsonrpc"])
		require.NotNil(t, parsed["error"])
	})

	t.Run("includes_data_when_present", func(t *testing.T) {
		t.Parallel()
		rpcErr := &rpcError{
			ID:      msgID{format: 1, Number: 1},
			Code:    methodNotFound,
			Message: "test error",
			Data:    json.RawMessage(`{"detail": "extra info"}`),
		}
		data, err := json.Marshal(rpcErr)
		require.NoError(t, err)
		require.Contains(t, string(data), "extra info")
	})
}

func TestMsgID_MarshalJSON_ZeroFormat(t *testing.T) {
	t.Parallel()

	t.Run("marshals_zero_format_as_empty_string", func(t *testing.T) {
		t.Parallel()
		// format 0 falls into default case which marshals the String field
		id := msgID{format: 0, String: ""}
		result, err := json.Marshal(id)
		require.NoError(t, err)
		require.Equal(t, `""`, string(result))
	})

	t.Run("marshals_zero_format_with_string_value", func(t *testing.T) {
		t.Parallel()
		id := msgID{format: 0, String: "custom-id"}
		result, err := json.Marshal(id)
		require.NoError(t, err)
		require.Equal(t, `"custom-id"`, string(result))
	})
}

func TestMsgID_UnmarshalJSON_Null(t *testing.T) {
	t.Parallel()

	t.Run("unmarshals_null_as_zero_int", func(t *testing.T) {
		t.Parallel()
		var id msgID
		err := json.Unmarshal([]byte("null"), &id)
		require.NoError(t, err)
		// null unmarshals as 0 for int64, so format becomes 1 (int format)
		require.Equal(t, byte(1), id.format)
		require.Equal(t, int64(0), id.Number)
	})
}
