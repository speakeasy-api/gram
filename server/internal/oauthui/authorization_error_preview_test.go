package oauthui_test

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

// TestAuthorizationErrorPagePreview renders the authorization error page for
// representative callbacks, asserting only that each one renders.
//
// It doubles as the visual-review aid: point GRAM_OAUTH_ERROR_PREVIEW_DIR at a
// directory and the rendered pages land there instead of in a temp dir.
//
//	GRAM_OAUTH_ERROR_PREVIEW_DIR=/tmp/oauth-error go test ./internal/oauthui/ -run TestAuthorizationErrorPagePreview
//
// then serve that directory and open index.html.
func TestAuthorizationErrorPagePreview(t *testing.T) {
	t.Parallel()

	outDir := os.Getenv("GRAM_OAUTH_ERROR_PREVIEW_DIR")
	if outDir == "" {
		outDir = t.TempDir()
	}

	pages := []struct {
		name, redirectURI, code, description string
	}{
		{"remote-client", "https://client.example/oauth/callback", oautherr.CodeInvalidRequest, "code_challenge is required (PKCE mandatory)"},
		{"native-client", "com.example.app://oauth/callback", oautherr.CodeInvalidScope, "requested scope is not supported"},
		{"long-destination", "https://a-very-long-subdomain-name-for-an-mcp-client.integrations.example.com/oauth/callback", oautherr.CodeUnsupportedResponseType, "response_type must be code"},
	}

	// The stylesheet's @font-face urls point at the server's font route, which
	// nothing serves here, so copy the embedded fonts to that path under the
	// output directory for a plain static server to resolve them.
	fontDir := filepath.Join(outDir, "mcp", "consent-fonts")
	require.NoError(t, os.MkdirAll(fontDir, 0o750))
	fonts, err := consentpage.FontNames()
	require.NoError(t, err)
	for _, name := range fonts {
		data, ok := consentpage.Font(name)
		require.True(t, ok, name)
		require.NoError(t, os.WriteFile(filepath.Join(fontDir, name), data, 0o600))
	}

	var index strings.Builder
	index.WriteString("<!doctype html><title>Authorization error page states</title><ul>")
	for _, page := range pages {
		callback := page.redirectURI + "?" + url.Values{
			oauthwire.ParamError:            {page.code},
			oauthwire.ParamErrorDescription: {page.description},
			oauthwire.ParamState:            {"preview-state"},
		}.Encode()
		response := httptest.NewRecorder()
		require.NoError(t, oauthui.WriteAuthorizationError(t.Context(), response, callback, page.code, page.description), page.name)
		require.NoError(t, os.WriteFile(filepath.Join(outDir, page.name+".html"), response.Body.Bytes(), 0o600))
		fmt.Fprintf(&index, `<li><a href="%s.html">%s</a></li>`, page.name, page.name)
	}
	index.WriteString("</ul>")
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "index.html"), []byte(index.String()), 0o600))
}
