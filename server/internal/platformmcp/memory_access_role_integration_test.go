package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestMemoryAccessRoleConfiguration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_memory_access_role")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: principal.OrganizationID, Name: organization.Name, Slug: organization.Slug,
		WorkosID: conv.ToPGText("workos-" + uuid.NewString()), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	organization, err = organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "memory-access-role-key")
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "memory-access-role-key", manager)
	require.NoError(t, err)

	created, err := service.Create(ctx, principal, CreateMCPAccessRoleInput{ProjectID: project.ID.String(), Name: "Team memory readers", Rules: []MCPAccessRoleRule{}, IdempotencyKey: "create-memory", Confirmed: true})
	require.NoError(t, err)
	roleID, err := reads.references.Decode(created.Role.Reference, principal, subjectKindAccessRole, reads.now())
	require.NoError(t, err)
	// An unrelated grant must survive every memory change.
	baseline := &accessgen.RoleGrant{Scope: "mcp:connect", Selectors: []*accessgen.Selector{{ResourceKind: "mcp", ResourceID: "*"}}}
	tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction required by the role manager API; statements use generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	updated, _, err := manager.UpdateRoleTx(ctx, tx, principal.OrganizationID, organization.WorkosID.String, access.RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)}, &accessgen.UpdateRolePayload{ID: roleID, AddGrants: []*accessgen.RoleGrant{baseline}})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	version, err := service.roleVersion(updated.After)
	require.NoError(t, err)

	input := ConfigureMemoryAccessRoleInput{ProjectID: project.ID.String(), Scope: "memory:read", RoleReference: created.Role.Reference, ExpectedVersion: version, Enabled: true, IdempotencyKey: "enable-memory-read", Confirmed: true}

	for _, bad := range []func(*ConfigureMemoryAccessRoleInput){
		func(in *ConfigureMemoryAccessRoleInput) { in.Confirmed = false },
		func(in *ConfigureMemoryAccessRoleInput) { in.Scope = "chat:read" },
		func(in *ConfigureMemoryAccessRoleInput) { in.Scope = "" },
	} {
		mutated := input
		bad(&mutated)
		_, err = service.ConfigureMemory(ctx, principal, mutated)
		require.ErrorIs(t, err, ErrAccessRoleMutationInvalid)
	}
	assistant := principal
	assistant.Surface = SurfaceProjectAssistant
	_, err = service.ConfigureMemory(ctx, assistant, input)
	require.ErrorIs(t, err, ErrAccessRoleMutationInvalid)
	wrongProject := input
	wrongProject.ProjectID = uuid.NewString()
	_, err = service.ConfigureMemory(ctx, principal, wrongProject)
	require.ErrorIs(t, err, ErrAccessRoleMutationNotFound)

	enabled, err := service.ConfigureMemory(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, enabled.ExactGrantPresent)
	require.Equal(t, "memory:read", enabled.Scope)
	require.False(t, enabled.Receipt.Replayed)
	replayed, err := service.ConfigureMemory(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, enabled.Receipt.ID, replayed.Receipt.ID)

	// The same key with a different scope is a different request.
	changedScope := input
	changedScope.Scope = "memory:write"
	_, err = service.ConfigureMemory(ctx, principal, changedScope)
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)

	// A stale version refuses; the fresh one adds memory:write beside read.
	write := changedScope
	write.IdempotencyKey = "enable-memory-write-stale"
	_, err = service.ConfigureMemory(ctx, principal, write)
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)
	write.IdempotencyKey = "enable-memory-write"
	write.ExpectedVersion = enabled.Role.Version
	writeEnabled, err := service.ConfigureMemory(ctx, principal, write)
	require.NoError(t, err)
	require.True(t, writeEnabled.ExactGrantPresent)

	stored, err := manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	require.True(t, hasMemoryAccessRoleGrant(stored, "memory:read", project.ID))
	require.True(t, hasMemoryAccessRoleGrant(stored, "memory:write", project.ID))
	require.Contains(t, stored.Grants, baseline)

	// Removing read leaves write and the unrelated grant alone.
	removeRead := input
	removeRead.Enabled = false
	removeRead.IdempotencyKey = "disable-memory-read"
	removeRead.ExpectedVersion = writeEnabled.Role.Version
	disabled, err := service.ConfigureMemory(ctx, principal, removeRead)
	require.NoError(t, err)
	require.False(t, disabled.ExactGrantPresent)
	stored, err = manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	require.False(t, hasMemoryAccessRoleGrant(stored, "memory:read", project.ID))
	require.True(t, hasMemoryAccessRoleGrant(stored, "memory:write", project.ID))
	require.Contains(t, stored.Grants, baseline)
}
