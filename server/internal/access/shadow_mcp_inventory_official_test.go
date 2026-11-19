package access

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	shadowmcprepo "github.com/speakeasy-api/gram/server/internal/shadowmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestService_ReadShadowMCPInventoryTarget_SuggestsThenReportsLinkedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ti.service.WithServerURL(ti.siteURL)

	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  authCtx.ProjectID.String(),
		ServerURL:  "https://github.example.com/mcp",
		ServerName: "Tracker",
		UserEmail:  "reader@example.com",
		ObservedAt: time.Now().UTC(),
	})

	// The project serves the same upstream URL through Speakeasy.
	official := seedAccessOfficialServer(t, ctx, ti, *authCtx.ProjectID, "Tracker (official)", "https://github.example.com/mcp/", "private")
	endpointSlug := "tracker-endpoint-" + uuid.NewString()
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: official.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            endpointSlug,
	})
	require.NoError(t, err)

	read := func() *gen.ShadowMCPInventoryServer {
		server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			TargetKind:     shadowMCPTargetKindServerURL,
			TargetKey:      "https://github.example.com/mcp",
		})
		require.NoError(t, err)
		return server
	}

	// A server that proxies to the same URL is only offered until linked.
	server := read()
	require.Nil(t, server.OfficialServer)
	require.NotNil(t, server.SuggestedOfficialServer)
	require.Equal(t, official.ID.String(), server.SuggestedOfficialServer.McpServerID)
	require.Equal(t, "Tracker (official)", server.SuggestedOfficialServer.Name)

	require.NoError(t, ti.service.SetShadowMCPOfficialServer(ctx, &gen.SetShadowMCPOfficialServerPayload{
		SessionToken: nil,
		ProjectID:    authCtx.ProjectID.String(),
		ServerURL:    "https://github.example.com/mcp",
		McpServerID:  new(official.ID.String()),
	}))

	server = read()
	require.Nil(t, server.SuggestedOfficialServer)
	require.NotNil(t, server.OfficialServer)
	require.Equal(t, official.ID.String(), server.OfficialServer.McpServerID)
	require.Equal(t, "Tracker (official)", server.OfficialServer.Name)
	require.Equal(t, official.Slug.String, conv.PtrValOr(server.OfficialServer.Slug, ""))
	require.Equal(t, "https://app.example.com/mcp/"+endpointSlug, conv.PtrValOr(server.OfficialServer.URL, ""))
	require.Nil(t, server.OfficialServer.GatewayName)

	// The list reports the same thing as the single read.
	list, err := ti.service.ReadShadowMCPInventory(ctx, ShadowMCPInventoryReadInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Limit:          50,
		Cursor:         nil,
	})
	require.NoError(t, err)
	require.Len(t, list.Servers, 1)
	require.NotNil(t, list.Servers[0].OfficialServer)
	require.Equal(t, official.ID.String(), list.Servers[0].OfficialServer.McpServerID)
}

func TestService_ReadShadowMCPInventoryTarget_NoOfficialServerByDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Unmanaged")
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  authCtx.ProjectID.String(),
		ServerURL:  "https://github.example.com/mcp",
		ServerName: "Unmanaged",
		UserEmail:  "reader@example.com",
		ObservedAt: time.Now().UTC(),
	})

	server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		TargetKind:     shadowMCPTargetKindServerURL,
		TargetKey:      "https://github.example.com/mcp",
	})
	require.NoError(t, err)
	require.Nil(t, server.OfficialServer)
}

// seedAccessOfficialServer creates a project MCP server in front of a remote
// source at remoteURL, the way the dashboard makes a server official.
func seedAccessOfficialServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, name string, remoteURL string, visibility string) mcpserversrepo.McpServer {
	t.Helper()
	remote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          conv.ToPGText(name),
		Slug:          conv.ToPGText("remote-" + uuid.NewString()),
		TransportType: "streamable-http",
		Url:           remoteURL,
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             projectID,
		Name:                  conv.ToPGText(name),
		Slug:                  conv.ToPGText("server-" + uuid.NewString()),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            visibility,
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return server
}

func TestService_SetShadowMCPOfficialServer_AddChangeRemoveAndAddBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")

	chosenID := seedAccessOfficialServer(t, ctx, ti, *authCtx.ProjectID, "Company Tools", "https://internal.example.com/mcp", "private").ID
	otherID := seedAccessOfficialServer(t, ctx, ti, *authCtx.ProjectID, "Other Tools", "https://other.example.com/mcp", "private").ID
	read := func() *gen.ShadowMCPInventoryServer {
		server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			TargetKind:     shadowMCPTargetKindServerURL,
			TargetKey:      "https://github.example.com/mcp",
		})
		require.NoError(t, err)
		return server
	}
	set := func(serverURL string, mcpServerID *string) {
		t.Helper()
		require.NoError(t, ti.service.SetShadowMCPOfficialServer(ctx, &gen.SetShadowMCPOfficialServerPayload{
			SessionToken: nil,
			ProjectID:    authCtx.ProjectID.String(),
			ServerURL:    serverURL,
			McpServerID:  mcpServerID,
		}))
	}
	count := func(action audit.Action) int64 {
		t.Helper()
		n, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
		require.NoError(t, err)
		return n
	}
	require.Nil(t, read().OfficialServer)

	// Add: an unrelated server becomes the official version.
	set("https://github.example.com/mcp/", new(chosenID.String()))
	official := read().OfficialServer
	require.NotNil(t, official)
	require.Equal(t, chosenID.String(), official.McpServerID)
	require.Equal(t, "Company Tools", official.Name)

	created, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionShadowMCPOfficialServerCreate)
	require.NoError(t, err)
	require.Equal(t, "https://github.example.com/mcp", created.SubjectDisplay)
	require.Equal(t, "shadow_mcp_official_server", created.SubjectType)
	after, err := audittest.DecodeAuditData(created.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, chosenID.String(), after["mcp_server_id"])
	require.NotContains(t, after, "allow_direct_access")
	linkID := created.SubjectID

	// Saving the same link again changes nothing and records nothing.
	set("https://github.example.com/mcp", new(chosenID.String()))
	require.Equal(t, int64(1), count(audit.ActionShadowMCPOfficialServerCreate))
	require.Equal(t, int64(0), count(audit.ActionShadowMCPOfficialServerUpdate))

	// Change: the link points at another server.
	set("https://github.example.com/mcp", new(otherID.String()))
	require.Equal(t, otherID.String(), read().OfficialServer.McpServerID)
	updated, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionShadowMCPOfficialServerUpdate)
	require.NoError(t, err)
	require.Equal(t, linkID, updated.SubjectID)
	before, err := audittest.DecodeAuditData(updated.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, chosenID.String(), before["mcp_server_id"])
	after, err = audittest.DecodeAuditData(updated.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, otherID.String(), after["mcp_server_id"])

	// Remove: the link is soft-deleted and the server is unmanaged again.
	set("https://github.example.com/mcp", nil)
	require.Nil(t, read().OfficialServer)
	deleted, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionShadowMCPOfficialServerDelete)
	require.NoError(t, err)
	require.Equal(t, linkID, deleted.SubjectID)
	before, err = audittest.DecodeAuditData(deleted.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, otherID.String(), before["mcp_server_id"])
	var deletedAt pgtype.Timestamptz
	require.NoError(t, ti.conn.QueryRow(ctx, "SELECT deleted_at FROM shadow_mcp_official_servers WHERE id = $1", linkID).Scan(&deletedAt))
	require.True(t, deletedAt.Valid, "removal soft-deletes the row")

	// Removing it again changes nothing and records nothing.
	set("https://github.example.com/mcp", nil)
	require.Equal(t, int64(1), count(audit.ActionShadowMCPOfficialServerDelete))

	// Add back: the removed row is restored, so the link keeps its id.
	set("https://github.example.com/mcp", new(chosenID.String()))
	require.Equal(t, chosenID.String(), read().OfficialServer.McpServerID)
	require.Equal(t, int64(2), count(audit.ActionShadowMCPOfficialServerCreate))
	restored, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionShadowMCPOfficialServerCreate)
	require.NoError(t, err)
	require.Equal(t, linkID, restored.SubjectID)
	var rows int
	require.NoError(t, ti.conn.QueryRow(ctx, "SELECT count(*) FROM shadow_mcp_official_servers WHERE project_id = $1", *authCtx.ProjectID).Scan(&rows))
	require.Equal(t, 1, rows)
}

// Two first links to one URL serialize on the URL: the second waits for the
// first to commit and records a change from the link it replaces.
func TestService_SetShadowMCPOfficialServer_ConcurrentFirstLinksAuditReplacedLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")
	first := seedAccessOfficialServer(t, ctx, ti, *authCtx.ProjectID, "First", "https://first.example.com/mcp", "private")
	second := seedAccessOfficialServer(t, ctx, ti, *authCtx.ProjectID, "Second", "https://second.example.com/mcp", "private")
	key := "https://github.example.com/mcp"

	// The first writer holds the URL lock and has not saved its link yet.
	holder := testenv.BeginTx(t, ctx, ti.conn)
	holderQueries := shadowmcprepo.New(holder)
	require.NoError(t, holderQueries.LockShadowMCPOfficialServerURL(ctx, shadowmcprepo.LockShadowMCPOfficialServerURLParams{ProjectID: *authCtx.ProjectID, ServerUrl: key}))

	done := make(chan error, 1)
	go func() {
		done <- ti.service.SetShadowMCPOfficialServer(ctx, &gen.SetShadowMCPOfficialServerPayload{
			SessionToken: nil,
			ProjectID:    authCtx.ProjectID.String(),
			ServerURL:    key,
			McpServerID:  new(second.ID.String()),
		})
	}()
	testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(holder), 1)

	_, err := holderQueries.UpsertShadowMCPOfficialServerChoice(ctx, shadowmcprepo.UpsertShadowMCPOfficialServerChoiceParams{
		ProjectID:   *authCtx.ProjectID,
		ServerUrl:   key,
		McpServerID: uuid.NullUUID{UUID: first.ID, Valid: true},
		UserID:      pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	require.NoError(t, holder.Commit(ctx))
	require.NoError(t, <-done)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionShadowMCPOfficialServerUpdate)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, first.ID.String(), before["mcp_server_id"], "the second link records the first as the link it replaced")
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, second.ID.String(), after["mcp_server_id"])
}

func TestService_SetShadowMCPOfficialServer_RejectsDisabledOrForeignServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)

	disabledID := seedAccessOfficialServer(t, ctx, ti, *authCtx.ProjectID, "Off", "https://off.example.com/mcp", "disabled").ID
	err := ti.service.SetShadowMCPOfficialServer(ctx, &gen.SetShadowMCPOfficialServerPayload{
		SessionToken: nil,
		ProjectID:    authCtx.ProjectID.String(),
		ServerURL:    "https://github.example.com/mcp",
		McpServerID:  new(disabledID.String()),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// A live server in another project of the same organization is not this
	// project's to link.
	otherProject := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	foreignID := seedAccessOfficialServer(t, ctx, ti, otherProject.ID, "Elsewhere", "https://elsewhere.example.com/mcp", "private").ID
	err = ti.service.SetShadowMCPOfficialServer(ctx, &gen.SetShadowMCPOfficialServerPayload{
		SessionToken: nil,
		ProjectID:    authCtx.ProjectID.String(),
		ServerURL:    "https://github.example.com/mcp",
		McpServerID:  new(foreignID.String()),
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	err = ti.service.SetShadowMCPOfficialServer(ctx, &gen.SetShadowMCPOfficialServerPayload{
		SessionToken: nil,
		ProjectID:    authCtx.ProjectID.String(),
		ServerURL:    "https://github.example.com/mcp",
		McpServerID:  new(uuid.NewString()),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Without a blocking policy the project is in the built-in blocklist mode:
// a server reads as allowed until an administrator denies it, then blocked.
// There is no "observed" in-between.
func TestService_ReadShadowMCPInventoryTarget_BuiltInBlocklist(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")
	read := func() *gen.ShadowMCPInventoryServer {
		server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			TargetKind:     shadowMCPTargetKindServerURL,
			TargetKey:      "https://github.example.com/mcp",
		})
		require.NoError(t, err)
		return server
	}

	server := read()
	require.Equal(t, shadowMCPInventoryAccessAllowed, server.Access)
	require.NotNil(t, server.AccessSummary)
	require.Equal(t, shadowMCPAccessStateAllowed, server.AccessSummary.State)

	request := seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://github.example.com/mcp", "denied", 0)
	seedApprovalDecisionRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, request.ID, "denied", []string{"user:all"})

	server = read()
	require.Equal(t, shadowMCPInventoryAccessBlocked, server.Access)
	require.Equal(t, shadowMCPAccessStateBlocked, server.AccessSummary.State)
	require.Equal(t, shadowMCPAccessCoverageFull, server.AccessSummary.DecisionCoverage)
}

// A blocked-by-default policy for some people leaves everyone else in the
// built-in blocklist. An unreviewed server is blocked for the audience only,
// so it reads restricted; a denied server is blocked for the audience by the
// policy and for everyone else by the denial, so it reads blocked.
func TestService_ReadShadowMCPInventoryTarget_TargetedPolicyAndBuiltInBlocklist(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")
	createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      authCtx.ProjectID.String(),
		Name:           "Shadow MCP",
		Action:         "block",
		Disposition:    "block_all",
		AudienceType:   "targeted",
	})
	read := func() *gen.ShadowMCPInventoryServer {
		server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			TargetKind:     shadowMCPTargetKindServerURL,
			TargetKey:      "https://github.example.com/mcp",
		})
		require.NoError(t, err)
		return server
	}

	require.Equal(t, shadowMCPInventoryAccessRestricted, read().Access)

	request := seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://github.example.com/mcp", "denied", 0)
	seedApprovalDecisionRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, request.ID, "denied", []string{"user:all"})

	require.Equal(t, shadowMCPInventoryAccessBlocked, read().Access)
}

// A denial narrowed to a role blocks that role only: in the built-in
// blocklist the server reads restricted, not blocked.
func TestService_ReadShadowMCPInventoryTarget_NarrowDenialIsRestricted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")

	request := seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://github.example.com/mcp", "denied", 0)
	seedApprovalDecisionRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, request.ID, "denied", []string{"role:contractor"})

	server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		TargetKind:     shadowMCPTargetKindServerURL,
		TargetKey:      "https://github.example.com/mcp",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessRestricted, server.Access)
}

// An approval narrowed to a role lets only that role through: in the
// built-in blocklist the server reads restricted.
func TestService_ReadShadowMCPInventoryTarget_NarrowApprovalIsRestricted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")

	request := seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://github.example.com/mcp", "approved", 0)
	seedApprovalDecisionRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, request.ID, "approved", []string{"role:engineer"})

	server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		TargetKind:     shadowMCPTargetKindServerURL,
		TargetKey:      "https://github.example.com/mcp",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessRestricted, server.Access)
	// The built-in blocklist enforces the approval itself, with no grants:
	// it is carried as recorded.
	require.Equal(t, shadowMCPAccessCoverageFull, server.AccessSummary.DecisionCoverage)
}

// An approval for everyone in the built-in blocklist writes no grants, yet
// it is what keeps the server allowed, so its coverage is full.
func TestService_ReadShadowMCPInventoryTarget_BuiltInApprovalIsFullyCarried(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")

	request := seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://github.example.com/mcp", "approved", 0)
	seedApprovalDecisionRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, request.ID, "approved", []string{"user:all"})

	server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		TargetKind:     shadowMCPTargetKindServerURL,
		TargetKey:      "https://github.example.com/mcp",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPAccessStateAllowed, server.AccessSummary.State)
	require.NotNil(t, server.AccessSummary.Decision)
	require.Equal(t, "approved", *server.AccessSummary.Decision)
	require.Equal(t, shadowMCPAccessCoverageFull, server.AccessSummary.DecisionCoverage)
}
