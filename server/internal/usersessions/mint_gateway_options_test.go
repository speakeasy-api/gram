package usersessions_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestMintGatewayDiscoveryOverride(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	gateway, issuerID := createIssuerGatedMintMetaServer(t, ctx, ti, "mint-mode")
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()))
	payload := &sessionsgen.MintUserSessionPayload{MetaMcpServerID: conv.PtrEmpty(gateway.ID.String()), DiscoveryMode: conv.PtrEmpty("direct")}
	got, err := ti.service.MintUserSession(ctx, payload)
	require.NoError(t, err)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(got.AccessToken, urn.NewUserSessionIssuer(issuerID).String())
	require.NoError(t, err)
	row, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: claims.ID})
	require.NoError(t, err)
	policy, err := toolfilter.ParseSessionPolicy(row.ToolSelection)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotNil(t, policy.Gateway)
	require.NotNil(t, policy.Gateway.DiscoveryMode)
	require.Equal(t, "meta_mcp_server:"+gateway.ID.String(), policy.Resource)
	require.Equal(t, metamcp.DiscoveryModeDirect, *policy.Gateway.DiscoveryMode)
	require.Nil(t, policy.Selection)

	payload.DiscoveryMode = nil
	got, err = ti.service.MintUserSession(ctx, payload)
	require.NoError(t, err)
	claims, err = usersessions.NewSigner("test-jwt-secret").Validate(got.AccessToken, urn.NewUserSessionIssuer(issuerID).String())
	require.NoError(t, err)
	row, err = repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: claims.ID})
	require.NoError(t, err)
	require.Empty(t, row.ToolSelection, "the gateway default must remain live, not copied into a session")
}

func TestMintGatewayCodeModeOverrideGate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	gateway, issuerID := createIssuerGatedMintMetaServer(t, ctx, ti, "mint-python")
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()))
	payload := &sessionsgen.MintUserSessionPayload{MetaMcpServerID: conv.PtrEmpty(gateway.ID.String()), DiscoveryMode: conv.PtrEmpty("code_mode")}
	_, err := ti.service.MintUserSession(ctx, payload)
	require.ErrorContains(t, err, "not available")
	ti.codeModeEnabled.Store(true)
	got, err := ti.service.MintUserSession(ctx, payload)
	require.NoError(t, err)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(got.AccessToken, urn.NewUserSessionIssuer(issuerID).String())
	require.NoError(t, err)
	// Store Code Mode as the gateway default while the runtime is selectable.
	_, err = metamcprepo.New(ti.conn).UpdateMetaMCPServer(ctx, metamcprepo.UpdateMetaMCPServerParams{
		Name:                 gateway.Name,
		DiscoveryMode:        conv.ToPGText(string(metamcp.DiscoveryModeCode)),
		UserSessionIssuerID:  gateway.UserSessionIssuerID,
		Visibility:           pgtype.Text{String: "", Valid: false},
		NetworkAccessModeSet: false,
		NetworkAccessMode:    pgtype.Text{String: "", Valid: false},
		InstructionsSet:      false,
		Instructions:         pgtype.Text{String: "", Valid: false},
		ID:                   gateway.ID,
		OrganizationID:       gateway.OrganizationID,
		ProjectID:            gateway.ProjectID,
	})
	require.NoError(t, err)
	ti.codeModeEnabled.Store(false)
	row, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: claims.ID})
	require.NoError(t, err)
	policy, err := toolfilter.ParseSessionPolicy(row.ToolSelection)
	require.NoError(t, err)
	require.Equal(t, metamcp.DiscoveryModeCode, *policy.Gateway.DiscoveryMode, "issued policy survives the runtime becoming unavailable")
	_, err = ti.service.MintUserSession(ctx, payload)
	require.ErrorContains(t, err, "not available", "new overrides remain gated")

	stored, err := metamcprepo.New(ti.conn).GetMetaMCPServer(ctx, metamcprepo.GetMetaMCPServerParams{ID: gateway.ID, OrganizationID: gateway.OrganizationID, ProjectID: gateway.ProjectID})
	require.NoError(t, err)
	require.Equal(t, string(metamcp.DiscoveryModeCode), stored.DiscoveryMode, "the gateway default is Code Mode")
	payload.DiscoveryMode = nil
	got, err = ti.service.MintUserSession(ctx, payload)
	require.NoError(t, err, "following the stored Code Mode default remains available")
	claims, err = usersessions.NewSigner("test-jwt-secret").Validate(got.AccessToken, urn.NewUserSessionIssuer(issuerID).String())
	require.NoError(t, err)
	row, err = repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: claims.ID})
	require.NoError(t, err)
	require.Empty(t, row.ToolSelection, "the session follows the stored default live rather than copying it")
}
