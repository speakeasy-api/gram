package auth

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
	"github.com/workos/workos-go/v6/pkg/workos_errors"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCallback_ProviderErrorsDoNotExposeCredentials(t *testing.T) {
	t.Parallel()
	const pendingToken = "pending-authentication-secret"
	const code = "authorization-code-secret"
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{
			name:   "organization selection",
			status: http.StatusForbidden, contentType: "application/json",
			body: `{"code":"organization_selection_required","message":"choose an organization","pending_authentication_token":"` + pendingToken + `","user":{"id":"user_test","email":"user@example.test"},"organizations":[]}`,
		},
		{
			name:   "provider echoes code",
			status: http.StatusBadRequest, contentType: "application/json",
			body: `{"error":"invalid_grant","error_description":"invalid code ` + code + `","pending_authentication_token":"` + pendingToken + `"}`,
		},
		{
			name:   "unstructured gateway error",
			status: http.StatusBadGateway, contentType: "text/plain",
			body: "request failed with " + code + " and " + pendingToken,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)
			client := usermanagement.NewClient("test-key")
			client.Endpoint = server.URL
			client.HTTPClient = server.Client()
			adapter := identity.NewWorkOSAdapter(client)

			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			resolver := identity.NewResolver(logger, provider, nil, server.URL, "client_test", adapter, nil, nil, nil, nil, nil, nil, "")

			_, err := resolver.ExchangeCodeForTokens(t.Context(), code)
			require.Error(t, err)
			require.EqualError(t, err, "identity provider code exchange failed")
			require.NotContains(t, fmt.Sprintf("%+v", err), pendingToken)
			if tc.name == "organization selection" {
				var selection *workos_errors.OrganizationSelectionRequiredError
				require.ErrorAs(t, err, &selection)
				require.Equal(t, pendingToken, selection.PendingAuthenticationToken, "typed continuation remains available for deliberate handling")
			}

			service := &Service{logger: logger, identity: resolver, cfg: AuthConfigurations{SignInRedirectURL: "https://app.example.test/login"}}
			result, err := service.Callback(t.Context(), &gen.CallbackPayload{Code: code})
			require.NoError(t, err)
			require.Equal(t, "https://app.example.test/login?signin_error=lookup_error", result.Location)
			location, err := url.Parse(result.Location)
			require.NoError(t, err)
			require.Equal(t, url.Values{"signin_error": {"lookup_error"}}, location.Query())
			require.Empty(t, result.SessionToken)
			require.Empty(t, result.SessionCookie)
			require.Contains(t, logs.String(), "identity provider code exchange failed")
			for _, secret := range []string{pendingToken, code, "pending_authentication_token"} {
				require.NotContains(t, result.Location, secret)
				require.NotContains(t, logs.String(), secret)
			}
			spans := recorder.Ended()
			require.Len(t, spans, 2)
			for _, span := range spans {
				require.Equal(t, codes.Error, span.Status().Code)
				require.Equal(t, "identity provider code exchange failed", span.Status().Description)
				for _, secret := range []string{pendingToken, code} {
					require.NotContains(t, fmt.Sprint(span.Attributes(), span.Events(), span.Status()), secret)
				}
			}
		})
	}
}

func TestCodeExchangeErrorPreservesCancellation(t *testing.T) {
	t.Parallel()
	// Cancellation still participates in typed error handling after sanitizing.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := usermanagement.NewClient("test-key")
	client.Endpoint = "http://127.0.0.1:1"
	adapter := identity.NewWorkOSAdapter(client)
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	resolver := identity.NewResolver(testenv.NewLogger(t), provider, nil, client.Endpoint, "client_test", adapter, nil, nil, nil, nil, nil, nil, "")
	_, err := resolver.ExchangeCodeForTokens(ctx, "unused-code")
	require.ErrorIs(t, err, context.Canceled)
	require.EqualError(t, err, "identity provider code exchange failed")
}

func TestMagicAuthProviderErrorsDoNotExposeCredentials(t *testing.T) {
	t.Parallel()
	const pendingToken = "pending-magic-authentication-secret"
	const code = "magic-authorization-code-secret"
	for _, failCreation := range []bool{true, false} {
		t.Run(fmt.Sprintf("creation=%t", failCreation), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/user_management/magic_auth" && !failCreation {
					_, _ = fmt.Fprintf(w, `{"email":"user@example.test","code":%q}`, code)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, `{"error":"invalid_grant","error_description":%q,"pending_authentication_token":%q}`, code, pendingToken)
			}))
			t.Cleanup(server.Close)
			client := usermanagement.NewClient("test-key")
			client.Endpoint = server.URL
			client.HTTPClient = server.Client()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			resolver := identity.NewResolver(testenv.NewLogger(t), provider, nil, server.URL, "client_test", identity.NewWorkOSAdapter(client), nil, nil, nil, nil, nil, nil, "")
			_, err := resolver.AuthenticateWithMagicAuth(t.Context(), "user@example.test")
			require.Error(t, err)
			for _, secret := range []string{pendingToken, code, "pending_authentication_token"} {
				require.NotContains(t, fmt.Sprintf("%+v", err), secret)
				for _, span := range recorder.Ended() {
					require.NotContains(t, fmt.Sprint(span.Attributes(), span.Events(), span.Status()), secret)
				}
			}
		})
	}
}
