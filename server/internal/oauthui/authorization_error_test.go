package oauthui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/html"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
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
			require.Contains(t, response.Body.String(), "Request could not be completed")
			require.NotContains(t, response.Body.String(), "href=")
			require.Empty(t, response.Header().Get("Location"))
		})
	}
}

func TestAuthorizationErrorPageFallbackSurvivesCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response := httptest.NewRecorder()
	err := oauthui.WriteAuthorizationError(ctx, response, oauthui.AuthorizationError{
		Status:      http.StatusForbidden,
		Code:        `<script>alert("code")</script>`,
		Description: `<img src=x onerror="alert('description')">`,
		CallbackURL: "https://client.example/callback",
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'", response.Header().Get("Content-Security-Policy"))
	require.Empty(t, response.Header().Get("Location"))
	body := response.Body.String()
	require.Contains(t, body, string(consentpage.Styles))
	require.Contains(t, body, "<svg")
	require.Contains(t, body, "Secured by Speakeasy.")
	require.Contains(t, body, "Request could not be completed")
	require.Contains(t, body, "&lt;script&gt;")
	require.Contains(t, body, "&lt;img")
	require.NotContains(t, body, "<script")
	require.NotContains(t, body, "<img")
	require.NotContains(t, body, "href=")
	require.NotContains(t, body, "https://client.example/callback")
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
			require.NoError(t, oauthui.WriteAuthorizationError(ctx, response, oauthui.AuthorizationError{CallbackURL: "https://client.example/cb?error=" + tc.code, Code: tc.code, Description: "developer description", ClientName: "Example Client", ClientID: "client-id", ClientIDOrigin: "metadata.example", ServerName: "acme-server"}))
			body := response.Body.String()
			require.Contains(t, body, tc.explanation)
			require.Contains(t, body, "Example Client")
			require.Contains(t, body, "acme-server")
			require.Contains(t, body, traceID.String())
			require.Contains(t, body, "developer description")
			require.Contains(t, body, "Returns to")
			require.Contains(t, body, "client.example")
			require.Contains(t, body, "Client verified from")
			require.Contains(t, body, "metadata.example")
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
	require.NoError(t, oauthui.WriteAuthorizationError(ctx, response, oauthui.AuthorizationError{CallbackURL: "https://client.example/cb?state=secret-state", Code: oautherr.CodeInvalidRequest, Description: `code_challenge is required <b>`, ClientName: "Example Client", ClientID: "client-id", ClientIDOrigin: "metadata.example", ServerName: "acme-server"}))
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
		"Client verified from: metadata.example",
		"MCP server: acme-server",
		"Returns to: client.example",
	}, "\n"), copied)
	require.NotContains(t, copied, "secret-state")
}

func TestAuthorizationErrorPageWithoutCallback(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			require.NoError(t, oauthui.WriteAuthorizationError(t.Context(), response, oauthui.AuthorizationError{Status: status, Code: oautherr.CodeInvalidRequest, Description: "public detail"}))
			body := oauthtest.RequireAuthorizationErrorPage(t, response, status)
			require.Equal(t, oautherr.CodeInvalidRequest, body.Code)
			require.Equal(t, "public detail", body.Description)
			require.NotContains(t, response.Body.String(), "Returns to")
			require.NotContains(t, response.Body.String(), "Return to")
			require.Contains(t, response.Body.String(), "close this tab and start again")
			require.Contains(t, response.Body.String(), "data-copy-details")
		})
	}
}

func TestAuthorizationErrorPageReasonOverridesCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		reason   oauthui.Reason
		approval bool
		want     string
	}{
		{"blocked", oauthui.ReasonOrganizationBlocked, false, "Your organization doesn't allow"},
		{"expired", oauthui.ReasonExpired, false, "link expired or was already used"},
		{"unavailable", oauthui.ReasonUnavailable, false, "This is a problem on our side"},
		{"session", oauthui.ReasonSessionChanged, false, "current browser or sign-in session"},
		{"approval-expired", oauthui.ReasonExpired, true, "This approval link expired"},
		{"approval-session", oauthui.ReasonSessionChanged, true, "Your staff sign-in session changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			require.NoError(t, oauthui.WriteAuthorizationError(t.Context(), response, oauthui.AuthorizationError{Code: oautherr.CodeInvalidClient, Reason: tc.reason, Approval: tc.approval, ClientName: "Example Client", ServerName: "example-server"}))
			body := oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusBadRequest)
			require.Equal(t, oautherr.CodeInvalidClient, body.Code)
			require.Contains(t, response.Body.String(), tc.want)
			require.NotContains(t, response.Body.String(), "before you signed in")
		})
	}
}

func TestAuthorizationErrorRecoveryLink(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		setup  bool
		reason oauthui.Reason
	}{
		{name: "setup", setup: true},
		{name: "canceled", reason: oauthui.ReasonCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const destination = "https://app.example.test/@self/setup"
			response := httptest.NewRecorder()
			require.NoError(t, oauthui.WriteAuthorizationError(t.Context(), response, oauthui.AuthorizationError{
				Setup: tc.setup, Reason: tc.reason, Status: http.StatusForbidden,
				Code: oautherr.CodeAccessDenied, Description: "forbidden",
				Recovery: &oauthui.RecoveryLink{URL: destination, Label: "Open <organization> setup"},
			}))
			body := oauthtest.RequireAuthorizationErrorRecovery(t, response, http.StatusForbidden, destination)
			require.Equal(t, oautherr.CodeAccessDenied, body.Code)
			require.Equal(t, "forbidden", body.Description)
			require.Contains(t, response.Body.String(), "Open &lt;organization&gt; setup")
			require.NotContains(t, response.Body.String(), "Return to app.example.test", "recovery must not use OAuth callback wording")
		})
	}
}

func TestAuthorizationErrorRejectsUnsafeRecoveryLink(t *testing.T) {
	t.Parallel()
	for _, target := range []string{
		"javascript:alert(1)", "data:text/html,example", "//untrusted.example/setup",
		"/@self/setup", "https://", "https://user:password@app.example.test/setup", "com.example.app:callback",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			err := oauthui.WriteAuthorizationError(t.Context(), response, oauthui.AuthorizationError{
				Status: http.StatusForbidden, Code: oautherr.CodeAccessDenied,
				Recovery: &oauthui.RecoveryLink{URL: target, Label: "Open organization setup"},
			})
			require.Error(t, err)
			require.Equal(t, http.StatusForbidden, response.Code)
			require.NotContains(t, response.Body.String(), "href=")
			require.Empty(t, response.Header().Get("Location"))
		})
	}
}
