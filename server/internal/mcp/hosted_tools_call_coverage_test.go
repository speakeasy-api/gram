package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestHostedMalformedToolsCall_RecordsCoverageAtMethodBoundary(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ctx = stampHostedCoverageUser(t, ctx, authCtx.UserID)
	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "hosted-coverage-toolset-"+uuid.NewString()[:8])
	endpointSlug := "hosted-coverage-endpoint-" + uuid.NewString()[:8]
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, uuid.Nil)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  "malformed params",
	})
	require.NoError(t, err)
	_, err = servePublicHTTP(t, ctx, ti, endpointSlug, body, "", nil)
	require.NoError(t, err)

	coveragePoints := collectKillswitchCoverage(t, reader)
	require.Equal(t, map[attribute.Set]int64{
		attribute.NewSet(
			attr.McpKillswitchSurface(mcpmetrics.KillswitchSurfaceHosted),
			attr.McpKillswitchIdentityClass(mcpmetrics.KillswitchIdentityActiveUser),
			attr.McpKillswitchResourceClass(mcpmetrics.KillswitchResourceCanonicalServer),
		): 1,
	}, coveragePoints)
}

type hostedCoverageNeverRevoked struct{}

func (hostedCoverageNeverRevoked) IsTokenRevoked(context.Context, string) (bool, error) {
	return false, nil
}

func stampHostedCoverageUser(t *testing.T, ctx context.Context, userID string) context.Context {
	t.Helper()
	signer := sessiontokens.NewSigner("hosted-coverage-test-secret")
	token, _, err := signer.Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject(userID),
		Audience: "hosted-coverage-test",
		Issuer:   "hosted-coverage-test",
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	proof, err := signer.ValidateBearer(ctx, token, "hosted-coverage-test", hostedCoverageNeverRevoked{})
	require.NoError(t, err)
	return mcpidentity.NewValidatorBoundary().StampValidatedSession(ctx, proof)
}
