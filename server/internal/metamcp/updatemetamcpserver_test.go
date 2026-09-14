package metamcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	srv "github.com/speakeasy-api/gram/server/gen/http/meta_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestUpdateMetaMcpServer_RenamesAndAttachesIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "before rename",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "after rename",
		UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, "after rename", updated.Name)
	require.NotNil(t, updated.UserSessionIssuerID)
	require.Equal(t, issuerID.String(), *updated.UserSessionIssuerID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

// An omitted issuer preserves the stored one (matching UpdateMCPServer's
// COALESCE), and an update can never strand a gateway issuer-less: a gateway
// that would end up without one gets one minted defensively.
func TestUpdateMetaMcpServer_OmittedIssuerPreservesReference(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "issuer holder",
		UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
	})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "issuer holder",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.UserSessionIssuerID)
	require.Equal(t, issuerID.String(), *updated.UserSessionIssuerID)
}

// A pre-defaults gateway row with no issuer gains a minted one on its next
// update rather than staying in the anonymous trap.
func TestUpdateMetaMcpServer_MintsIssuerWhenNoneStored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	row, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                "legacy issuerless",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          "private",
	})
	require.NoError(t, err)
	require.False(t, row.UserSessionIssuerID.Valid)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  row.ID.String(),
		Name:                "legacy issuerless",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.UserSessionIssuerID, "update must mint an issuer for an issuerless gateway")
}

func TestUpdateMetaMcpServer_NonPublicOmissionFailsClosedAndPublicRecoverySucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "network recovery"})
	require.NoError(t, err)
	rows, err := testrepo.New(ti.conn).SetMetaMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMetaMCPServerNetworkAccessModeFixtureParams{
		NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true},
		ID:                uuid.MustParse(created.ID),
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	_, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		ID: created.ID, Name: created.Name, NetworkAccessMode: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	publicOnly := types.NetworkAccessMode("public_only")
	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		ID: created.ID, Name: created.Name, NetworkAccessMode: &publicOnly,
	})
	require.NoError(t, err)
	require.Equal(t, publicOnly, updated.NetworkAccessMode)

	stored, err := metamcprepo.New(ti.conn).GetMetaMCPServer(ctx, metamcprepo.GetMetaMCPServerParams{
		ID: uuid.MustParse(created.ID), OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.False(t, stored.NetworkAccessMode.Valid)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "private_only", beforeSnapshot["NetworkAccessMode"])
	require.Equal(t, "public_only", afterSnapshot["NetworkAccessMode"])
}

func TestUpdateMetaMcpServer_UnknownStoredModeFailsClosedAndPublicRecoverySucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "unknown network recovery"})
	require.NoError(t, err)

	rows, err := testrepo.New(ti.conn).SetMetaMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMetaMCPServerNetworkAccessModeFixtureParams{
		NetworkAccessMode: pgtype.Text{String: "future_mode", Valid: true},
		ID:                uuid.MustParse(created.ID),
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	_, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		ID: created.ID, Name: created.Name, NetworkAccessMode: nil,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)

	publicOnly := types.NetworkAccessMode("public_only")
	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		ID: created.ID, Name: created.Name, NetworkAccessMode: &publicOnly,
	})
	require.NoError(t, err)
	require.Equal(t, publicOnly, updated.NetworkAccessMode)
}

func TestUpdateMetaMcpServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  uuid.NewString(),
		Name:                "ghost",
		UserSessionIssuerID: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateMetaMcpServer_OmittedVisibilityIsPreserved(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	disabled := types.MetaMcpServerVisibility(metamcp.VisibilityDisabled)
	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "stays disabled",
		UserSessionIssuerID: nil,
		Visibility:          &disabled,
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "renamed while disabled",
		UserSessionIssuerID: nil,
		Visibility:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, "renamed while disabled", updated.Name)
	require.Equal(t, disabled, updated.Visibility)
}

func TestUpdateMetaMcpServer_ChangesVisibility(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "to disable",
		UserSessionIssuerID: nil,
		Visibility:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, types.MetaMcpServerVisibility(metamcp.VisibilityPrivate), created.Visibility)

	disabled := types.MetaMcpServerVisibility(metamcp.VisibilityDisabled)
	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                created.Name,
		UserSessionIssuerID: nil,
		Visibility:          &disabled,
	})
	require.NoError(t, err)
	require.Equal(t, disabled, updated.Visibility)
}

// A gateway's consent wiring binds member provider clients to a specific
// issuer, so changing the gateway's issuer must re-run member attachment
// against the new one rather than silently orphaning every provider tile.
func TestUpdateMetaMcpServer_RewiresProviderClientsOnIssuerChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	meta := seedMetaMcpServer(t, ctx, ti, "issuer rewire host")
	require.NotNil(t, meta.UserSessionIssuerID, "create mints the gateway issuer")

	rsRepo := remotesessionsrepo.New(ti.conn)
	remoteIssuer, err := rsRepo.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(projectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              "rewire-rsi-" + uuid.NewString()[:8],
		Issuer:                            "https://as.example.com/" + uuid.NewString(),
		Name:                              conv.ToPGTextEmpty(""),
		LogoAssetID:                       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientSetupDocumentationUrl:       conv.ToPGTextEmpty(""),
		AuthorizationEndpoint:             conv.ToPGTextEmpty(""),
		TokenEndpoint:                     conv.ToPGTextEmpty(""),
		RevocationEndpoint:                conv.ToPGTextEmpty(""),
		RegistrationEndpoint:              conv.ToPGTextEmpty(""),
		JwksUri:                           conv.ToPGTextEmpty(""),
		ServiceDocumentation:              conv.ToPGTextEmpty(""),
		OpPolicyUri:                       conv.ToPGTextEmpty(""),
		OpTosUri:                          conv.ToPGTextEmpty(""),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		ClientIDMetadataDocumentSupported: false,
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	client, err := rsRepo.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(projectID),
		OrganizationID:          conv.ToPGText(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   remoteIssuer.ID,
		ClientID:                "rewire-client",
		ClientSecretEncrypted:   conv.ToPGTextEmpty(""),
		ClientIDIssuedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGTextEmpty(""),
		Scope:                   []string{},
		Audience:                conv.ToPGTextEmpty(""),
		LegacyCallbackUrl:       false,
	})
	require.NoError(t, err)

	serverID := seedMcpServer(t, ctx, ti.conn, projectID)
	memberServer, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.True(t, memberServer.UserSessionIssuerID.Valid)
	require.NoError(t, rsRepo.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   memberServer.UserSessionIssuerID.UUID,
	}))
	stamped, err := testrepo.New(ti.conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: conv.ToNullUUID(remoteIssuer.ID),
		ID:                    serverID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped)

	boundCount := func(issuerID uuid.UUID) int {
		rows, lerr := rsRepo.ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
			UserSessionIssuerID: issuerID,
			ProjectID:           conv.ToNullUUID(projectID),
			OrganizationID:      conv.ToPGText(authCtx.ActiveOrganizationID),
		})
		require.NoError(t, lerr)
		return len(rows)
	}

	_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, boundCount(uuid.MustParse(*meta.UserSessionIssuerID)),
		"add binds the member's client to the original issuer")

	newIssuerID := seedUserSessionIssuer(t, ctx, ti.conn, projectID)
	_, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  meta.ID,
		Name:                meta.Name,
		UserSessionIssuerID: conv.PtrEmpty(newIssuerID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, 1, boundCount(newIssuerID),
		"issuer change must re-bind member provider clients to the new issuer")

	// A second swap wires the members again: rewiring is not a one-shot.
	thirdIssuerID := seedUserSessionIssuer(t, ctx, ti.conn, projectID)
	_, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  meta.ID,
		Name:                meta.Name,
		UserSessionIssuerID: conv.PtrEmpty(thirdIssuerID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, 1, boundCount(thirdIssuerID),
		"each issuer change must re-wire member provider clients")
}

// Instructions follow omit-preserves / blank-clears: an update that does not
// mention them leaves the stored value alone, and a blank submission restores
// the built-in gateway instructions (NULL) rather than serving an empty
// handshake.
func TestUpdateMetaMcpServer_Instructions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "instructed gateway",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)
	require.Nil(t, created.Instructions)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                created.Name,
		UserSessionIssuerID: nil,
		Instructions:        conv.PtrEmpty("  Call list_servers before anything else.  "),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Instructions)
	require.Equal(t, "Call list_servers before anything else.", *updated.Instructions)

	fetchedWithInstructions, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, updated.Instructions, fetchedWithInstructions.Instructions)
	listed, err := ti.service.ListMetaMcpServers(ctx, &gen.ListMetaMcpServersPayload{})
	require.NoError(t, err)
	require.Len(t, listed.MetaMcpServers, 1)
	require.Equal(t, updated.Instructions, listed.MetaMcpServers[0].Instructions)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Call list_servers before anything else.", afterSnapshot["Instructions"])

	preserved, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "renamed gateway",
		UserSessionIssuerID: nil,
		Instructions:        nil,
	})
	require.NoError(t, err)
	require.NotNil(t, preserved.Instructions)
	require.Equal(t, "Call list_servers before anything else.", *preserved.Instructions)

	cleared, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "renamed gateway",
		UserSessionIssuerID: nil,
		Instructions:        conv.PtrEmpty("   "),
	})
	require.NoError(t, err)
	require.Nil(t, cleared.Instructions)

	fetched, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	})
	require.NoError(t, err)
	require.Nil(t, fetched.Instructions)
}

// Goa accepts a JSON \u0000 but Postgres text rejects NUL, so the handler
// strips it instead of surfacing a storage error.
func TestUpdateMetaMcpServer_InstructionsStripNUL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "nul gateway",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                created.Name,
		UserSessionIssuerID: nil,
		Instructions:        conv.PtrEmpty("Call list_servers\x00 first."),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Instructions)
	require.Equal(t, "Call list_servers first.", *updated.Instructions)
}

func TestUpdateMetaMcpServer_InstructionsNormalizedLength(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "length gateway"})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, input, want string
		invalid           bool
	}{
		{name: "unicode at limit with padding", input: " \t" + strings.Repeat("界", 10000) + "\x00\n", want: strings.Repeat("界", 10000)},
		{name: "over limit", input: strings.Repeat("界", 10001), invalid: true},
		{name: "blank over raw limit", input: strings.Repeat(" \x00", 10001)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := &srv.UpdateMetaMcpServerRequestBody{ID: &created.ID, Name: &created.Name, Instructions: &tc.input}
			require.NoError(t, srv.ValidateUpdateMetaMcpServerRequestBody(body))
			updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: created.ID, Name: created.Name, Instructions: &tc.input})
			if tc.invalid {
				require.ErrorContains(t, err, "instructions must not exceed 10000")
				return
			}
			require.NoError(t, err)
			if tc.want == "" {
				require.Nil(t, updated.Instructions)
			} else {
				require.Equal(t, &tc.want, updated.Instructions)
			}
		})
	}
}

// instructions_mode follows omit-preserves via COALESCE; the effective mode
// reads as append while the column is NULL.
func TestUpdateMetaMcpServer_InstructionsMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "mode gateway",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)
	require.Equal(t, types.MetaMcpInstructionsMode("append"), created.InstructionsMode)

	replace := types.MetaMcpInstructionsMode("replace")
	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                created.Name,
		UserSessionIssuerID: nil,
		InstructionsMode:    &replace,
	})
	require.NoError(t, err)
	require.Equal(t, replace, updated.InstructionsMode)

	preserved, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "renamed mode gateway",
		UserSessionIssuerID: nil,
		InstructionsMode:    nil,
	})
	require.NoError(t, err)
	require.Equal(t, replace, preserved.InstructionsMode)

	fetched, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	})
	require.NoError(t, err)
	require.Equal(t, replace, fetched.InstructionsMode)
}
