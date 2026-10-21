package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// authorityLookupFailureTracer fails only the live authority lookup after
// authorization has resolved and validated the registered client.
type authorityLookupFailureTracer struct {
	hits atomic.Int32
}

func (tr *authorityLookupFailureTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: GetLiveNetworkIngressAuthority ") {
		tr.hits.Add(1)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		return canceled
	}
	return ctx
}

func (*authorityLookupFailureTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestAuthorizeAuthorityLookupUnavailable(t *testing.T) {
	t.Parallel()
	tracer := &authorityLookupFailureTracer{}
	logger := testenv.NewLogger(t)
	ctx, ti := newTestMCPServiceWithPoolConfig(t, logger, testenv.NewMeterProvider(t),
		&mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil,
		func(config *pgxpool.Config) { config.ConnConfig.Tracer = tracer }, mcp.MetaRuntimeConfig{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, logger, toolset.McpSlug.String, mcproute.MCP)
	require.NoError(t, err)
	origin := requestorigin.Origin{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://private.example.ts.net", OrganizationID: endpoint.OrganizationID, NetworkIngressID: uuid.New()}
	query := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {client.RedirectUris[0]}, "code_challenge": {strings.Repeat("a", 43)}, "code_challenge_method": {"S256"}}
	request := httptest.NewRequest(http.MethodGet, origin.BaseURL+"/mcp/"+endpoint.Slug+"/authorize?"+query.Encode(), nil)
	request = request.WithContext(requestorigin.WithContext(ctx, origin))
	response := httptest.NewRecorder()
	err = ti.service.ServeAuthorize(response, request, endpoint)
	require.ErrorIs(t, err, networkingress.ErrAuthorityUnavailable)
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.EqualValues(t, 1, tracer.hits.Load())
	require.Empty(t, response.Header().Get("Location"))
	require.Empty(t, response.Header().Get("WWW-Authenticate"))
}
