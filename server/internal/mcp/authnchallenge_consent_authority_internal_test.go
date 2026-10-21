package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestConsentActionLiveAuthorityUnavailableMetric(t *testing.T) {
	t.Parallel()
	for _, unavailable := range []bool{true, false} {
		name := "unavailable"
		if !unavailable {
			name = "terminal_request_mismatch"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := browserTestService(t)
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
			s.metrics = mcpmetrics.NewMetrics(provider.Meter("test"), s.logger)
			// Closed pools fail locally, without depending on network timing or a live DB.
			pool, err := pgxpool.New(t.Context(), "postgres://localhost/unused")
			require.NoError(t, err)
			pool.Close()
			s.db = pool
			endpoint := &ResolvedMcpEndpoint{OrganizationID: "test-organization", UserSessionIssuerID: uuid.New(), Slug: "test", RouteBase: mcproute.MCP}
			state := browserTestState()
			state.CSRFToken = "known-form-token"
			state.UserSessionIssuerID = endpoint.UserSessionIssuerID
			origin := requestorigin.Origin{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://private.example.ts.net", OrganizationID: endpoint.OrganizationID, NetworkIngressID: uuid.New()}
			state.Endpoint = EndpointRef{McpSlug: endpoint.Slug, RouteBase: endpoint.RouteBase, BaseURL: origin.BaseURL, Authority: networkingress.Authority{
				Surface: origin.Surface, BaseURL: origin.BaseURL, OrganizationID: origin.OrganizationID, NetworkIngressID: origin.NetworkIngressID, NamespaceKind: networkingress.NamespacePlatform,
			}}
			ctx := requestorigin.WithContext(t.Context(), origin)
			require.NoError(t, endpoint.ValidateChallenge(ctx, state.Endpoint, state.UserSessionIssuerID))
			require.NoError(t, s.authnChallengeCache.Store(ctx, state))
			if !unavailable {
				origin.NetworkIngressID = uuid.New()
				ctx = requestorigin.WithContext(t.Context(), origin)
			}
			form := url.Values{"state": {state.ID}, "csrf_token": {state.CSRFToken}, "action": {"retry_delegation"}}
			req := httptest.NewRequest(http.MethodPost, origin.BaseURL+"/mcp/test/connect/remote-session", strings.NewReader(form.Encode())).WithContext(ctx)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(federatedBrowserCookie(state.Browser.CookieID, "origin-browser", 600))
			response := httptest.NewRecorder()
			err = s.ServeConsentAction(response, req, endpoint)
			var failure *oops.ShareableError
			require.ErrorAs(t, err, &failure)
			expectedCode := oops.CodeBadRequest
			if unavailable {
				expectedCode = oops.CodeUnavailable
				require.ErrorIs(t, err, networkingress.ErrAuthorityUnavailable)
			}
			require.Equal(t, expectedCode, failure.Code)
			require.Empty(t, response.Header().Get("Location"))
			_, err = s.authnChallengeCache.Get(ctx, "authnChallenge:"+state.ID)
			require.NoError(t, err, "authority errors preserve retry state")
			var data metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(ctx, &data))
			var count int64
			for _, scope := range data.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name != "oauth.authority.unavailable" {
						continue
					}
					sum, ok := metric.Data.(metricdata.Sum[int64])
					require.True(t, ok)
					for _, point := range sum.DataPoints {
						require.Equal(t, attribute.NewSet(attr.UserSessionIssuerID(endpoint.UserSessionIssuerID.String()), attr.ToolsetMCPSlug(endpoint.Slug), attr.OAuthFlowStage(string(mcpmetrics.OAuthFlowStageConsent))), point.Attributes)
						count += point.Value
					}
				}
			}
			expectedCount := int64(0)
			if unavailable {
				expectedCount = 1
			}
			require.Equal(t, expectedCount, count, "only transient authority failures count at consent stage")
		})
	}
}

func TestConsentChallengeCacheErrors(t *testing.T) {
	t.Parallel()
	for _, handler := range []struct {
		name  string
		serve func(*Service, http.ResponseWriter, *http.Request, *ResolvedMcpEndpoint) error
	}{
		{name: "action", serve: (*Service).ServeConsentAction},
		{name: "get", serve: (*Service).serveConsentGet},
		{name: "post", serve: (*Service).serveConsentPost},
		{name: "mcp", serve: (*Service).ServeConsentMCP},
		{name: "shared", serve: func(s *Service, _ http.ResponseWriter, r *http.Request, _ *ResolvedMcpEndpoint) error {
			_, err := s.sharedChallengeEndpoint(r.Context(), &sharedIssuer{logger: s.logger}, "unknown")
			return err
		}},
	} {
		t.Run(handler.name, func(t *testing.T) {
			t.Parallel()
			for _, unavailable := range []bool{false, true} {
				name := "missing"
				if unavailable {
					name = "unavailable"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					service := browserTestService(t)
					expected := oops.CodeBadRequest
					description := "authn challenge state not found or expired"
					if unavailable {
						service.authnChallengeCache = cache.NewTypedObjectCache[AuthnChallengeState](service.logger, nil, cache.SuffixNone)
						expected = oops.CodeUnavailable
						description = "authn challenge state is temporarily unavailable"
					}
					request := httptest.NewRequest(http.MethodPost, "/mcp/test/connect?state=unknown", strings.NewReader("state=unknown"))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					request.Header.Set(consentStateHeader, "unknown")
					request.Header.Set(consentCSRFHeader, "test-csrf")
					request.Header.Set(consentAttemptHeader, uuid.NewString())
					request = request.WithContext(requestorigin.WithContext(t.Context(), requestorigin.Origin{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://private.example.ts.net"}))
					response := httptest.NewRecorder()
					endpoint := &ResolvedMcpEndpoint{Slug: "test", RouteBase: mcproute.MCP, McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
					err := handler.serve(service, response, request, endpoint)
					var failure *oops.ShareableError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, expected, failure.Code)
					require.Contains(t, failure.Error(), description)
					require.Empty(t, response.Header().Get("WWW-Authenticate"))
				})
			}
		})
	}
}
