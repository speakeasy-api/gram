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
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

// TestAuthorizationErrorPagePreview renders the authorization error page for
// each kind of error and naming edge case, asserting only that each one renders.
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
		name, redirectURI string
		page              oauthui.AuthorizationError
	}{
		{"setup-invalid", "", oauthui.AuthorizationError{Setup: true, Status: 400, Code: oautherr.CodeInvalidRequest, Description: "missing intent parameter"}},
		{"setup-forbidden", "", oauthui.AuthorizationError{Setup: true, Status: 403, Code: oautherr.CodeAccessDenied, Description: "forbidden"}},
		{"setup-unavailable", "", oauthui.AuthorizationError{Setup: true, Status: 500, Code: oautherr.CodeServerError, Description: "internal error"}},
		{"assistant-canceled", "", oauthui.AuthorizationError{Status: 400, Reason: oauthui.ReasonCanceled, ClientName: "Speakeasy Assistant"}},
		{"blocked-no-callback", "", oauthui.AuthorizationError{Code: oautherr.CodeInvalidClient, Reason: oauthui.ReasonOrganizationBlocked, Description: "This client is blocked by organization policy", ClientName: "Example Client", ServerName: "example-observability"}},
		{"expired-no-callback", "", oauthui.AuthorizationError{Code: "bad_request", Reason: oauthui.ReasonExpired, Description: "authorization state not found or expired"}},
		{"approval-expired", "", oauthui.AuthorizationError{Status: 403, Code: "Forbidden", Reason: oauthui.ReasonExpired, Approval: true, Description: "approval expired or changed"}},
		{"unknown-client", "", oauthui.AuthorizationError{Code: oautherr.CodeInvalidClient, Reason: oauthui.ReasonInvalidClient, Description: "unknown client_id", ClientID: "unknown-client"}},
		{"unavailable-no-callback", "", oauthui.AuthorizationError{Status: 503, Code: oautherr.CodeTemporarilyUnavailable, Description: "client metadata is temporarily unavailable"}},
		{"incompatible", "https://cursor.example/oauth/callback", oauthui.AuthorizationError{Code: oautherr.CodeInvalidRequest, Description: "code_challenge is required (PKCE mandatory)", ClientName: "Cursor", ClientID: "8f14e45f-ceea-467f-a8c1-9f3e3b1c2d4a", ServerName: "acme-datadog"}},
		{"wrong-server", "https://cursor.example/oauth/callback", oauthui.AuthorizationError{Code: oautherr.CodeInvalidTarget, Description: `resource does not identify this MCP server (expected "https://mcp.example.com/mcp/acme-datadog")`, ClientName: "Cursor", ClientID: "8f14e45f-ceea-467f-a8c1-9f3e3b1c2d4a", ServerName: "acme-datadog"}},
		{"not-allowed", "https://chat.example/oauth/callback", oauthui.AuthorizationError{Code: oautherr.CodeAccessDenied, Description: "client is not permitted", ClientName: "Example Chat", ClientID: "https://chat.example/oauth/client.json", ClientIDOrigin: "chat.example", ServerName: "acme-datadog"}},
		{"unavailable", "https://cursor.example/oauth/callback", oauthui.AuthorizationError{Code: oautherr.CodeTemporarilyUnavailable, Description: "organization access could not be verified", ClientName: "Cursor", ClientID: "8f14e45f-ceea-467f-a8c1-9f3e3b1c2d4a", ServerName: "Speakeasy"}},
		{"other", "https://cursor.example/oauth/callback", oauthui.AuthorizationError{Code: "vendor_specific_error", Description: "something unexpected", ClientName: "Cursor", ClientID: "8f14e45f-ceea-467f-a8c1-9f3e3b1c2d4a", ServerName: "acme-datadog"}},
		{"unnamed-client-and-server", "com.example.app://oauth/callback", oauthui.AuthorizationError{Code: oautherr.CodeInvalidScope, Description: "requested scope is not supported", ClientID: "client_2b7c"}},
		{"long-names", "https://a-very-long-subdomain-name-for-an-mcp-client.integrations.example.com/oauth/callback", oauthui.AuthorizationError{Code: oautherr.CodeUnsupportedResponseType, Description: "response_type must be one of [code]", ClientName: "An Unusually Long MCP Client Display Name", ClientID: "8f14e45f-ceea-467f-a8c1-9f3e3b1c2d4a", ServerName: "acme-corporation-internal-observability-and-incident-tools"}},
	}

	// A fixed trace so the Reference row renders.
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
		TraceState: trace.TraceState{},
		Remote:     false,
	}))

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
		if page.redirectURI != "" {
			page.page.CallbackURL = page.redirectURI + "?" + url.Values{
				oauthwire.ParamError:            {page.page.Code},
				oauthwire.ParamErrorDescription: {page.page.Description},
				oauthwire.ParamState:            {"preview-state"},
			}.Encode()
		}
		response := httptest.NewRecorder()
		require.NoError(t, oauthui.WriteAuthorizationError(ctx, response, page.page), page.name)
		require.NoError(t, os.WriteFile(filepath.Join(outDir, page.name+".html"), response.Body.Bytes(), 0o600))
		fmt.Fprintf(&index, `<li><a href="%s.html">%s</a></li>`, page.name, page.name)
	}
	index.WriteString("</ul>")
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "index.html"), []byte(index.String()), 0o600))
}
