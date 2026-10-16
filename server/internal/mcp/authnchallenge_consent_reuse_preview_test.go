package mcp

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsentReusePagePreview(t *testing.T) {
	t.Parallel()
	endpoint := &ResolvedMcpEndpoint{RouteBase: "mcp", Slug: "example"}
	state := AuthnChallengeState{ID: "preview-state", CSRFToken: "preview-csrf"}
	w := httptest.NewRecorder()
	require.NoError(t, renderConsentReuse(w, endpoint, state))
	require.Contains(t, w.Body.String(), "Checking saved approval")
	require.Contains(t, w.Body.String(), "Review access")
	require.NotContains(t, w.Body.String(), "Give Access")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	if outDir := os.Getenv("GRAM_CONSENT_PREVIEW_DIR"); outDir != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(outDir, "mcp"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "reuse.html"), w.Body.Bytes(), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, strings.TrimPrefix(consentToolsScriptURL, "/")), consentToolsScriptData, 0o600))
	}
}
