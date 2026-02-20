package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
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
