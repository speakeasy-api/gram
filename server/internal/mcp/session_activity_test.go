package mcp

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPeekSessionActivityMethod(t *testing.T) {
	t.Parallel()

	request := &http.Request{Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{}}`))}
	require.Equal(t, "tools/call", peekSessionActivityMethod(request))

	restored, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","method":"tools/call","params":{}}`, string(restored))
}

func TestPeekSessionActivityMethodFindsMethodAfterLargeParams(t *testing.T) {
	t.Parallel()

	body := `{"params":"` + strings.Repeat("x", 64<<10) + `","method":"tools/call"}`
	request := &http.Request{Body: io.NopCloser(strings.NewReader(body))}
	require.Equal(t, "tools/call", peekSessionActivityMethod(request))

	restored, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(restored))
}

func TestPeekSessionActivityMethodDoesNotClassifyOversizedBody(t *testing.T) {
	t.Parallel()

	body := `{"method":"tools/call","params":"` + strings.Repeat("x", 1024) + `"}`
	request := &http.Request{Body: io.NopCloser(strings.NewReader(body))}
	require.Empty(t, peekSessionActivityMethodWithLimit(request, 128))

	buffered, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.Len(t, buffered, 129)
}

func TestPeekSessionActivityMethodRejectsMalformedBody(t *testing.T) {
	t.Parallel()

	request := &http.Request{Body: io.NopCloser(strings.NewReader(`{"method":`))}
	require.Empty(t, peekSessionActivityMethod(request))
}
