package usersessions_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

type testGatewayInventory struct{ snapshot *toolfilter.FrozenToolset }

func (p *testGatewayInventory) GatewayInventory(context.Context, uuid.UUID, uuid.UUID, urn.SessionSubject) (*toolfilter.FrozenToolset, error) {
	return p.snapshot, nil
}
func TestFrozenGatewayMintRechecksReviewedInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	gateway, issuerID := createIssuerGatedMintMetaServer(t, ctx, ti, "freeze-mint")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()))
	tool, err := toolfilter.NewFrozenTool("member--read", uuid.New(), "route", json.RawMessage(`{"name":"read","inputSchema":{"type":"object"}}`))
	require.NoError(t, err)
	ti.gatewayInventory.snapshot = &toolfilter.FrozenToolset{Tools: []toolfilter.FrozenTool{tool}}
	previewPayload := &gen.PreviewGatewayToolsetPayload{MetaMcpServerID: gateway.ID.String()}
	_, err = ti.service.PreviewGatewayToolset(ctx, previewPayload)
	require.ErrorContains(t, err, "not available")
	ti.features.SetFlag(feature.FlagGatewayFrozenToolsets, authCtx.ActiveOrganizationID, true)
	review, err := ti.service.PreviewGatewayToolset(ctx, previewPayload)
	require.NoError(t, err)
	require.Len(t, review.Tools, 1)
	payload := &gen.MintFrozenGatewaySessionPayload{MetaMcpServerID: gateway.ID.String(), FrozenToolset: &gen.FrozenGatewayReview{Fingerprint: review.Fingerprint, Tools: []string{tool.Name}}}
	minted, err := ti.service.MintFrozenGatewaySession(ctx, payload)
	require.NoError(t, err)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(minted.AccessToken, urn.NewUserSessionIssuer(issuerID).String())
	require.NoError(t, err)
	row, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: claims.ID})
	require.NoError(t, err)
	policy, err := toolfilter.ParseSessionPolicy(row.ToolSelection)
	require.NoError(t, err)
	require.Nil(t, policy.Gateway.DiscoveryMode, "freezing does not pin the gateway default")
	require.True(t, policy.Gateway.Frozen.Allows(tool))
	changed, err := toolfilter.NewFrozenTool(tool.Name, tool.MemberID, "replacement-route", tool.Definition)
	require.NoError(t, err)
	ti.gatewayInventory.snapshot.Tools[0] = changed
	_, err = ti.service.MintFrozenGatewaySession(ctx, payload)
	require.ErrorContains(t, err, "review again")
	ti.features.SetFlag(feature.FlagGatewayFrozenToolsets, authCtx.ActiveOrganizationID, false)
	_, err = ti.service.MintFrozenGatewaySession(ctx, payload)
	require.ErrorContains(t, err, "not available")
}
func TestFrozenGatewayReviewRequiresConnectionPermission(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	gateway, _ := createIssuerGatedMintMetaServer(t, ctx, ti, "freeze-denied")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.features.SetFlag(feature.FlagGatewayFrozenToolsets, authCtx.ActiveOrganizationID, true)
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err := ti.service.PreviewGatewayToolset(ctx, &gen.PreviewGatewayToolsetPayload{MetaMcpServerID: gateway.ID.String()})
	require.ErrorContains(t, err, "connection permission required")
}
