package oauthui_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/html"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

func TestAuthorizationErrorPagePreservesCallbackAndEscapesText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ uri, destination string }{
		{"https://client.example:8443/cb", "client.example:8443"},
		{"com.example.app://callback", "com.example.app://callback"},
		{"com.example.app:callback", "com.example.app:"},
	} {
		t.Run(tc.uri, func(t *testing.T) {
			t.Parallel()
			description := `unsupported method "<script>alert(1)</script>"`
			callback := tc.uri + "?" + url.Values{
				oauthwire.ParamError:            {oautherr.CodeInvalidRequest},
				oauthwire.ParamErrorDescription: {description},
				oauthwire.ParamState:            {`state&"<payload>`},
				"iss":                           {"https://issuer.example/mcp/server"},
			}.Encode()
			response := httptest.NewRecorder()
			require.NoError(t, oauthui.WriteAuthorizationError(t.Context(), response, oauthui.AuthorizationError{CallbackURL: callback, Code: oautherr.CodeInvalidRequest, Description: description, ClientName: `<b>client</b>`, ClientID: "client-id", ClientIDOrigin: "", ServerName: "acme-server"}))
			parsed := oauthtest.RequireAuthorizationErrorCallback(t, response, http.StatusBadRequest)
			require.Equal(t, callback, parsed.String())
			require.Contains(t, response.Body.String(), "Return to "+tc.destination)
			require.NotContains(t, response.Body.String(), "<script>")
			require.NotContains(t, response.Body.String(), "#ZgotmplZ")
			require.Contains(t, response.Body.String(), "&lt;script&gt;")
			require.NotContains(t, response.Body.String(), "<b>client</b>")
			require.Contains(t, response.Header().Get("Content-Security-Policy"), "default-src 'none'")
		})
	}
}

func TestAuthorizationErrorPageRejectsUnsafeSchemes(t *testing.T) {
	t.Parallel()
	for _, scheme := range []string{"javascript", "data", "vbscript", "file", "blob", "view-source"} {
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			err := oauthui.WriteAuthorizationError(t.Context(), response, oauthui.AuthorizationError{CallbackURL: strings.ToUpper(scheme) + ":payload", Code: oautherr.CodeInvalidRequest, Description: "invalid request", ClientName: "", ClientID: "", ClientIDOrigin: "", ServerName: ""})
			require.Error(t, err)
			require.Empty(t, response.Body.String())
			require.Empty(t, response.Header().Get("Location"))
		})
	}
}

func TestAuthorizationErrorPageExplainsErrorForEndUsers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ code, explanation string }{
		{oautherr.CodeInvalidRequest, "started sign-in in a way"},
		{oautherr.CodeUnsupportedResponseType, "started sign-in in a way"},
		{oautherr.CodeInvalidScope, "started sign-in in a way"},
		{oautherr.CodeInvalidTarget, "is set up with an address that doesn"},
		{oautherr.CodeAccessDenied, "allowed to connect to"},
		{oautherr.CodeInvalidClient, "allowed to connect to"},
		{oautherr.CodeServerError, "This is a problem on our side"},
		{oautherr.CodeTemporarilyUnavailable, "This is a problem on our side"},
		{"vendor_specific", "couldn"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			traceID := trace.TraceID{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19}
			ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled, TraceState: trace.TraceState{}, Remote: false}))
			response := httptest.NewRecorder()
			require.NoError(t, oauthui.WriteAuthorizationError(ctx, response, oauthui.AuthorizationError{CallbackURL: "https://client.example/cb?error=" + tc.code, Code: tc.code, Description: "developer description", ClientName: "Example Client", ClientID: "client-id", ClientIDOrigin: "client.example", ServerName: "acme-server"}))
			body := response.Body.String()
			require.Contains(t, body, tc.explanation)
			require.Contains(t, body, "Example Client")
			require.Contains(t, body, "acme-server")
			require.Contains(t, body, traceID.String())
			require.Contains(t, body, "developer description")
			require.Contains(t, body, "client.example")
		})
	}
}

func TestAuthorizationErrorPageNamesUnknownClientAndServerGenerically(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	require.NoError(t, oauthui.WriteAuthorizationError(t.Context(), response, oauthui.AuthorizationError{CallbackURL: "https://client.example/cb", Code: oautherr.CodeInvalidRequest, Description: "", ClientName: "", ClientID: "", ClientIDOrigin: "", ServerName: ""}))
	body := response.Body.String()
	require.Contains(t, body, "Your MCP client")
	require.Contains(t, body, "this MCP server")
	require.NotContains(t, body, ">Reference<")
}

func TestAuthorizationErrorPageCopiesTechnicalDetailsAsPlainText(t *testing.T) {
	t.Parallel()
	traceID := trace.TraceID{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19}
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled, TraceState: trace.TraceState{}, Remote: false}))
	response := httptest.NewRecorder()
	require.NoError(t, oauthui.WriteAuthorizationError(ctx, response, oauthui.AuthorizationError{CallbackURL: "https://client.example/cb?state=secret-state", Code: oautherr.CodeInvalidRequest, Description: `code_challenge is required <b>`, ClientName: "Example Client", ClientID: "client-id", ClientIDOrigin: "", ServerName: "acme-server"}))
	oauthtest.RequireAuthorizationErrorCallback(t, response, http.StatusBadRequest)

	doc, err := html.Parse(strings.NewReader(response.Body.String()))
	require.NoError(t, err)
	copied := ""
	for node := range doc.Descendants() {
		for _, attr := range node.Attr {
			if attr.Key == "data-copy-text" {
				copied = attr.Val
			}
		}
	}
	require.Equal(t, strings.Join([]string{
		"Speakeasy authorization error",
		"Reference: " + traceID.String(),
		"Time: " + strings.SplitN(strings.SplitN(copied, "Time: ", 2)[1], "\n", 2)[0],
		"Error: invalid_request",
		"Error description: code_challenge is required <b>",
		"MCP client: Example Client",
		"MCP client ID: client-id",
		"MCP server: acme-server",
		"Returns to: client.example",
	}, "\n"), copied)
	require.NotContains(t, copied, "secret-state")
}
