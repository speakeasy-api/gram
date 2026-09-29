package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	provisioningrepo "github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestReceiptBackedRoleUpdateSerializesWithReconciliation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_role_lock_order")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: principal.OrganizationID, Name: organization.Name, Slug: organization.Slug,
		WorkosID: conv.ToPGText("workos-" + uuid.NewString()), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	rows, err := platformrepo.New(conn).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID: principal.OrganizationID, ConnectionID: uuid.NullUUID{}, ConnectionGeneration: uuid.NullUUID{}, SkipAuthorizationFilter: true,
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())),
		ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, AfterMcpID: uuid.NullUUID{}, QueryText: "", ReadinessState: pgtype.Text{}, LimitValue: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	targetID := rows[0].McpServerID.String()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-role-integration-key")
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)

	created, err := service.Create(ctx, principal, CreateMCPAccessRoleInput{
		ProjectID: project.ID.String(), Name: "Concurrent operators", Description: "",
		Rules: []MCPAccessRoleRule{{MCPID: targetID}}, IdempotencyKey: "create-concurrent-role", Confirmed: true,
	})
	require.NoError(t, err)
	roleID, err := reads.references.Decode(created.Role.Reference, principal, subjectKindAccessRole, reads.now().UTC())
	require.NoError(t, err)
	roleUUID, err := uuid.Parse(roleID)
	require.NoError(t, err)

	// Hold reconciliation's first lock so the actual receipt-backed update must
	// queue. Its project/organization FK locks must not prevent this lock order.
	blocker, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: controlled transaction for deterministic lock interleaving
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return blocker.Rollback(ctx) })
	_, err = provisioningrepo.New(blocker).LockOrganization(ctx, principal.OrganizationID)
	require.NoError(t, err)
	blockerPID := int(blocker.Conn().PgConn().PID())
	input := UpdateMCPAccessRoleInput{
		ProjectID: project.ID.String(), RoleReference: created.Role.Reference, ExpectedVersion: created.Role.Version,
		AddRules:    []MCPAccessRoleRule{{MCPID: targetID, Disposition: "read_only"}},
		RemoveRules: []MCPAccessRoleRule{{MCPID: targetID}}, IdempotencyKey: "update-concurrent-role", Confirmed: true,
	}
	type updateResult struct {
		output UpdateMCPAccessRoleOutput
		err    error
	}
	updated := make(chan updateResult, 1)
	go func() {
		output, err := service.Update(ctx, principal, input)
		updated <- updateResult{output: output, err: err}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting) //nolint:glint // notestingrawsql: observe actual update transaction wait edge
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)

	// A reconciler already holding organization intent must still acquire the
	// role. Previously the queued update held that role while waiting for us.
	lockCtx, cancelLock := context.WithTimeout(ctx, 2*time.Second)
	defer cancelLock()
	_, err = provisioningrepo.New(blocker).LockOrganizationRoleLiveness(lockCtx, provisioningrepo.LockOrganizationRoleLivenessParams{ID: roleUUID, OrganizationID: principal.OrganizationID})
	require.NoError(t, err, "receipt-backed update must not hold the role before organization intent")

	reconciled := make(chan error, 1)
	go func() {
		// No provisioning configuration: still exercise the real organization's
		// role-liveness locking before reconciliation returns its skipped result.
		_, err := roleprovisioning.New(conn, audit.NewLogger(), nil, plugins.PublicationRequests{}).Reconcile(ctx, principal.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+roleID).String(), roleprovisioning.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), PublicationUserID: principal.UserID})
		reconciled <- err
	}()
	require.NoError(t, blocker.Commit(ctx))
	result := <-updated
	require.NoError(t, result.err)
	require.NoError(t, <-reconciled)
	require.False(t, result.output.Receipt.Replayed)
	require.NotEqual(t, created.Role.Version, result.output.Role.Version)
	replay, err := service.Update(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, result.output.Receipt.ID, replay.Receipt.ID)
	input.IdempotencyKey = "stale-concurrent-role"
	_, err = service.Update(ctx, principal, input)
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)
}
