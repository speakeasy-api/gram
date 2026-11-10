package mcp_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func callGatewayTool(t *testing.T, ti *testInstance, slug, token, name string, arguments map[string]any) string {
	t.Helper()
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": name, "arguments": arguments}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	return w.Body.String()
}
