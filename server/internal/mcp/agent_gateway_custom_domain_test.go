package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// An organization whose custom domain has an IP allowlist reaches its agent
// gateway only on that domain. This drives the real route through the
// custom-domain middleware by Host, as the custom-domain ingress delivers it,
// so the request carries the domain context that exempts it from the lockdown
// the platform host still applies.
func TestAgentGatewayRoute_AllowlistedCustomDomainHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domains := customdomainsrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "agent-route-" + uuid.NewString()[:8] + ".example.com",
		IpAllowlist:    []string{"203.0.113.0/24"},
	})
	require.NoError(t, err)
	_, err = domains.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{ID: domain.ID, Verified: true, Activated: true})
	require.NoError(t, err)

	serverURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	mux.Use(customdomains.Middleware(testenv.NewLogger(t), ti.conn, "prod", serverURL, nil))
	mcp.Attach(mux, ti.service.Service, nil)
	handler := middleware.PublicHTTPTracing("test")(mux)

	serve := func(host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "https://"+host+"/agent-mcp/"+fx.agent.ID.String(), bytes.NewReader(makeInitializeBody())).WithContext(t.Context())
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+fx.token)
		req.Header.Set("X-Client-Ip", "203.0.113.7")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	onDomain := serve(domain.Domain)
	require.Equal(t, http.StatusOK, onDomain.Code, onDomain.Body.String())
	var response runtimeInitializeResponse
	require.NoError(t, json.Unmarshal(onDomain.Body.Bytes(), &response))
	require.NotEmpty(t, response.Result.ProtocolVersion)

	onPlatform := serve(serverURL.Host)
	require.Equal(t, http.StatusForbidden, onPlatform.Code, onPlatform.Body.String())
	require.Contains(t, onPlatform.Body.String(), "only accessible via its custom domain")
}
