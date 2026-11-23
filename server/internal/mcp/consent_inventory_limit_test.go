package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// newLargeInventoryTestService lets a consent capture of thousands of tools
// finish. Under the race detector one such capture takes over a second of CPU,
// so on a loaded runner it can outlast the production gateway listing time and
// report the upstream as not responding.
func newLargeInventoryTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	return newTestMCPServiceWithPoolConfig(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     0,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	}, nil, nil, mcp.MetaRuntimeConfig{MemberCallTimeout: 0, ValidationTimeout: 0, GatewayListingTimeout: 30 * time.Second, AutoVerifyWait: 0, RecheckInterval: 0})
}

func TestGatewayLargeInventoryApproval(t *testing.T) {
	t.Parallel()
	ctx, ti := newLargeInventoryTestService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "large-catalog-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1500)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var listed struct {
		Result struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &listed))
	require.Len(t, listed.Result.Tools, 1500)
	result := callGatewayTool(t, ti, slug, connection.token, "source--tool_1499", map[string]any{})
	require.NotContains(t, result, `"isError":true`)
	require.Contains(t, result, "executed live")
}

func TestGatewayOverLimitExplainsRecovery(t *testing.T) {
	t.Parallel()
	ctx, ti := newLargeInventoryTestService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "over-limit-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 10001)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	page := consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.Contains(t, page, "This gateway has too many tools to approve. Ask an administrator to reduce the tool inventory.")
	require.NotContains(t, page, "Try again later")
}

func TestGatewayLargeSelectedInventoryApproval(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"manual", "annotation"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newLargeInventoryTestService(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "selected-catalog-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
			upstream := newCatalogUpstream(t, toolfilter.MaxTools)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
			var selected []string
			var annotations []string
			if kind == "manual" {
				for i := range toolfilter.MaxTools {
					selected = append(selected, fmt.Sprintf("source--tool_%03d", i))
				}
			} else {
				annotations = []string{toolfilter.AnnotationReadOnly}
			}
			connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, selected, annotations...)
			response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var listed struct {
				Result struct {
					Tools []json.RawMessage `json:"tools"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &listed))
			require.Len(t, listed.Result.Tools, toolfilter.MaxTools)
			result := callGatewayTool(t, ti, slug, connection.token, fmt.Sprintf("source--tool_%03d", toolfilter.MaxTools-1), map[string]any{})
			require.NotContains(t, result, `"isError":true`)
			require.Contains(t, result, "executed live")
		})
	}
}

func TestGatewayLargeInventoryConsentBoundary(t *testing.T) {
	t.Parallel()
	for _, specific := range []bool{false, true} {
		t.Run(fmt.Sprintf("specific=%t", specific), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newLargeInventoryTestService(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "bounded-catalog-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
			upstream := newCatalogUpstream(t, toolfilter.MaxTools-1)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
			var selected []string
			if specific {
				for i := range toolfilter.MaxTools - 2 {
					selected = append(selected, fmt.Sprintf("source--tool_%03d", i))
				}
			}
			connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, selected)
			require.Contains(t, callGatewayTool(t, ti, slug, connection.token, "source--tool_000", map[string]any{}), "executed live")
			before := upstream.calls.Load()
			if specific {
				unselected := fmt.Sprintf("source--tool_%03d", toolfilter.MaxTools-2)
				require.Contains(t, callGatewayTool(t, ti, slug, connection.token, unselected, map[string]any{}), `"isError":true`)
			}
			upstream.future.Store(true)
			newTool := fmt.Sprintf("source--tool_%03d", toolfilter.MaxTools-1)
			require.Contains(t, callGatewayTool(t, ti, slug, connection.token, newTool, map[string]any{}), `"isError":true`)
			require.Equal(t, before, upstream.calls.Load(), "unapproved tools never reach the upstream")
		})
	}
}
