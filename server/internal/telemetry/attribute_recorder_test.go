package telemetry

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/stretchr/testify/require"
)

func TestRecordRequestBodyContent(t *testing.T) {
	t.Parallel()

	h := HTTPLogAttributes{}
	body := []byte(`{"name":"test","value":42}`)
	h.RecordRequestBodyContent(body)

	val, ok := h[attr.GenAIToolCallArgumentsKey]
	require.True(t, ok)
	require.Equal(t, string(body), val)
}

func TestRecordResponseBodyContent(t *testing.T) {
	t.Parallel()

	h := HTTPLogAttributes{}
	body := []byte(`{"result":"success"}`)
	h.RecordResponseBodyContent(body)

	val, ok := h[attr.GenAIToolCallResultKey]
	require.True(t, ok)
	require.Equal(t, string(body), val)
}

func TestTruncateBodyUnderLimit(t *testing.T) {
	t.Parallel()

	body := []byte("short content")
	result := truncateBody(body)
	require.Equal(t, "short content", result)
}

func TestTruncateBodyAtExactLimit(t *testing.T) {
	t.Parallel()

	body := make([]byte, maxBodyContentBytes)
	for i := range body {
		body[i] = 'a'
	}
	result := truncateBody(body)
	require.Equal(t, string(body), result)
	require.Len(t, result, maxBodyContentBytes)
}

func TestTruncateBodyOverLimit(t *testing.T) {
	t.Parallel()

	originalSize := maxBodyContentBytes + 1000
	body := make([]byte, originalSize)
	for i := range body {
		body[i] = 'b'
	}
	result := truncateBody(body)

	require.True(t, len(result) > maxBodyContentBytes, "truncated result should include the marker")
	require.True(t, strings.HasPrefix(result, strings.Repeat("b", maxBodyContentBytes)))
	require.Contains(t, result, "...[truncated, original size:")
	require.Contains(t, result, "66536 bytes]")
}

func TestTruncateBodyEmpty(t *testing.T) {
	t.Parallel()

	result := truncateBody([]byte{})
	require.Equal(t, "", result)
}

func TestRecordBodyContentDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	h := HTTPLogAttributes{}
	h.RecordRequestBodyContent([]byte("first"))
	h.RecordRequestBodyContent([]byte("second"))

	val := h[attr.GenAIToolCallArgumentsKey]
	require.Equal(t, "second", val, "second call should overwrite the first")
}
