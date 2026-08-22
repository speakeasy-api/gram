package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRegistrationServiceRegistersRemoteURLEndToEnd(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_url_registration")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 3})
	require.NoError(t, err)
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store).
		WithRemoteRegistration(remoteRegistrationTestKey, NewPostgresRemoteMCPApprovals(conn))

	remoteURL := "https://remote.example.test/mcp"
	result, err := service.RegisterRemoteMCP(ctx, principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, remoteURL, time.Now().UTC()),
		IdempotencyKey: "remote-registration-key",
	})
	require.NoError(t, err)
	require.Equal(t, remoteURL, result.RemoteURL)
	require.False(t, result.BlockedPendingApproval, "no blocking policy exists, so enforcement is inactive")
	require.Empty(t, result.DashboardApprovalsURL)

	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       remoteURLSourceKind,
		CatalogProvider:  remoteURLCatalogProvider,
		CatalogReference: remoteURL,
	})
	require.NoError(t, err)
	require.Equal(t, registration.ID.String(), result.Registration)
	require.Equal(t, registrationStatusRegistered, registration.Status)
	require.True(t, registrationComponentsComplete(registration))

	remote, err := remotemcprepo.New(conn).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        registration.RemoteMcpServerID.UUID,
		ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Equal(t, remoteURL, remote.Url)
	require.Equal(t, "streamable-http", remote.TransportType)
	require.Equal(t, "remote.example.test source", remote.Name.String)

	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        registration.McpServerID.UUID,
		ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "remote.example.test", server.Name.String, "an omitted display name derives from the remote host")

	auditRecord, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionPlatformMcpRegistrationCreate)
	require.NoError(t, err)
	require.Equal(t, project.ID, auditRecord.ProjectID.UUID)
	require.Equal(t, "platform_mcp_registration", auditRecord.SubjectType)
	metadata, err := audittest.DecodeAuditData(auditRecord.Metadata)
	require.NoError(t, err)
	require.Equal(t, remoteURLCatalogProvider, metadata["catalog_provider"])
	require.Equal(t, remoteURL, metadata["catalog_reference"])
	auditCount, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPlatformMcpRegistrationCreate)
	require.NoError(t, err)

	replayed, err := service.RegisterRemoteMCP(ctx, principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, remoteURL, time.Now().UTC()),
		IdempotencyKey: "remote-registration-key",
	})
	require.NoError(t, err)
	require.Equal(t, result.Registration, replayed.Registration, "idempotency-key replay returns the original registration")
	require.True(t, replayed.Receipt.Replayed)
	auditCountAfterReplay, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPlatformMcpRegistrationCreate)
	require.NoError(t, err)
	require.Equal(t, auditCount, auditCountAfterReplay, "a replay records no second create audit event")
}

// TestRegistrationServiceRemoteRegistrationRespectsApprovalEnforcement walks
// one registration through the enforcement states the checker reads from real
// policies and grants: no policy → unblocked; block-by-default policy →
// blocked pending approval; recorded bypass grant → unblocked; allow-by-default
// policy carrying a block rule → blocked again.
func TestRegistrationServiceRemoteRegistrationRespectsApprovalEnforcement(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_url_enforcement")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 3})
	require.NoError(t, err)
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store).
		WithRemoteRegistration(remoteRegistrationTestKey, NewPostgresRemoteMCPApprovals(conn))

	remoteURL := "https://guarded.example.test/mcp"
	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(remoteURL)
	require.True(t, ok)
	register := func() RegisterRemoteMCPResult {
		t.Helper()
		result, err := service.RegisterRemoteMCP(ctx, principal, RegisterRemoteMCPInput{
			ProjectSlug:    project.Slug,
			ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, remoteURL, time.Now().UTC()),
			IdempotencyKey: "guarded-registration-key",
		})
		require.NoError(t, err)
		return result
	}

	initial := register()
	require.False(t, initial.BlockedPendingApproval, "no blocking policy means no enforcement to consult")

	blockAllPolicy, err := riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:                   uuid.New(),
		ProjectID:            project.ID,
		OrganizationID:       principal.OrganizationID,
		Name:                 "Block shadow MCP by default",
		Sources:              []string{"shadow_mcp"},
		Enabled:              true,
		Action:               "block",
		AudienceType:         "everyone",
		ShadowMcpDisposition: conv.ToPGTextEmpty(shadowmcp.DispositionBlockAll),
		AutoName:             false,
	})
	require.NoError(t, err)

	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	blocked := register()
	require.True(t, blocked.BlockedPendingApproval, "block-by-default enforcement blocks an unapproved server")
	require.Equal(t, initial.Registration, blocked.Registration, "enforcement changes the reported state, not the registration")
	require.Equal(t, "/"+organization.Slug+"/projects/"+project.Slug+"/shadow-mcp", blocked.DashboardApprovalsURL)

	// An admin approval reconciles into a bypass grant on the canonical URL;
	// the registration replay must read as approved from then on.
	require.NoError(t, policybypass.ReplacePolicyURLAudience(ctx, conn, principal.OrganizationID, authz.ScopeRiskPolicyBypass, blockAllPolicy.ID.String(), inventoryURL.CanonicalURL, []urn.Principal{authz.AllUsersPrincipal()}))
	approved := register()
	require.False(t, approved.BlockedPendingApproval, "a standing bypass grant is a recorded approval")
	require.Empty(t, approved.DashboardApprovalsURL)

	// An allow-by-default policy blocks through explicit block rules instead;
	// one naming this server must block it even while the block-all policy's
	// bypass grant stands.
	allowAllPolicy, err := riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:                   uuid.New(),
		ProjectID:            project.ID,
		OrganizationID:       principal.OrganizationID,
		Name:                 "Allow shadow MCP by default",
		Sources:              []string{"shadow_mcp"},
		Enabled:              true,
		Action:               "block",
		AudienceType:         "everyone",
		ShadowMcpDisposition: conv.ToPGTextEmpty(shadowmcp.DispositionAllowAll),
		AutoName:             false,
	})
	require.NoError(t, err)
	require.NoError(t, policybypass.ReplacePolicyURLAudience(ctx, conn, principal.OrganizationID, authz.ScopeRiskPolicyBlock, allowAllPolicy.ID.String(), inventoryURL.CanonicalURL, []urn.Principal{authz.AllUsersPrincipal()}))
	reBlocked := register()
	require.True(t, reBlocked.BlockedPendingApproval, "an allow-by-default block rule blocks the server")
}
