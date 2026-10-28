package mcp_test

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

// The shared token endpoint refuses an ID-JAG request naming no resource
// before the grant handler; it is still one ID-JAG rejection, logged once and
// counted.
func TestSharedAuthorizationServer_IDJAGWithoutRequestResourceIsRecorded(t *testing.T) {
	t.Parallel()
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false, ReplaceAttr: nil}))
	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, logger, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     0,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	}, nil)
	authCtx := requireProjectAuthContext(t, ctx)
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, sharedTestUpstreamIssuer, "https://identity.example.test/keys")
	setIssuerMode(t, ctx, ti, issuer.ID, "shared", "")
	client := seedCIMDClient(t, ctx, ti, issuer.ID, codexCIMDClientID)

	w := sharedToken(t, ctx, ti, issuer.ID.String(), url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamClientID: {client.ClientID},
		oauthwire.ParamAssertion: {"unused.assertion.value"},
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "invalid_request", decodeSharedTokenResponse(t, w).Error)

	reason := string(mcpmetrics.IDJAGRejectionFromValidator(idjag.ReasonResourceNotIndicated))
	var idJAGLines []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(logs.String()))
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		require.NotEqual(t, "shared authorization server refused resource indicator", line["msg"], "no second, generic line")
		if line["msg"] == "oauth ID-JAG token request rejected" {
			idJAGLines = append(idJAGLines, line)
		}
	}
	require.Len(t, idJAGLines, 1)
	require.Equal(t, string(mcpmetrics.IDJAGExchangeRejected), idJAGLines[0][string(attr.OAuthIDJAGOutcomeKey)])
	require.Equal(t, reason, idJAGLines[0][string(attr.OAuthIDJAGRejectionReasonKey)])
	require.Equal(t, issuer.ID.String(), idJAGLines[0][string(attr.UserSessionIssuerIDKey)])

	require.Equal(t, map[string]int64{string(mcpmetrics.IDJAGExchangeRejected) + "/" + reason: 1}, idJAGExchangeCounts(t, reader, issuer.ID))
}
