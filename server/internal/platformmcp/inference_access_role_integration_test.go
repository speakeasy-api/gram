package platformmcp

import (
	"testing"
	"time"

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

func TestInferenceAccessRoleConfiguration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_access_role_mutations")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: principal.OrganizationID, Name: organization.Name, Slug: organization.Slug,
		WorkosID: conv.ToPGText("workos-" + uuid.NewString()), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	reads := NewAccessReadService(logger, conn, allowBudget(), "access-role-integration-key")
	manager := access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger(), plugins.PublicationRequests{Enabled: false}, admission.NewGuard())
	service, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "access-role-integration-key", manager)
	require.NoError(t, err)

	created, err := service.Create(ctx, principal, CreateMCPAccessRoleInput{ProjectID: project.ID.String(), Name: "Workload inference", Rules: []MCPAccessRoleRule{}, IdempotencyKey: "create-inference", Confirmed: true})
	require.NoError(t, err)
	// Seed an unrelated grant: the narrow inference mutation must preserve it.
	roleID, err := reads.references.Decode(created.Role.Reference, principal, subjectKindAccessRole, reads.now())
	require.NoError(t, err)
	baseline := &accessgen.RoleGrant{Scope: "mcp:connect", Selectors: []*accessgen.Selector{{ResourceKind: "mcp", ResourceID: "*"}}}
	tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction required by the role manager API; statements use generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	organization, err = organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	updated, _, err := manager.UpdateRoleTx(ctx, tx, principal.OrganizationID, organization.WorkosID.String, access.RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)}, &accessgen.UpdateRolePayload{ID: roleID, AddGrants: []*accessgen.RoleGrant{baseline}})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	created.Role.Version, err = service.roleVersion(updated.After)
	require.NoError(t, err)
	input := ConfigureInferenceAccessRoleInput{ProjectID: project.ID.String(), RoleReference: created.Role.Reference, ExpectedVersion: created.Role.Version, Enabled: true, IdempotencyKey: "enable-inference", Confirmed: true}
	unconfirmed := input
	unconfirmed.Confirmed = false
	_, err = service.ConfigureInference(ctx, principal, unconfirmed)
	require.Error(t, err)
	assistant := principal
	assistant.Surface = SurfaceProjectAssistant
	_, err = service.ConfigureInference(ctx, assistant, input)
	require.ErrorIs(t, err, ErrAccessRoleMutationInvalid)
	wrongProject := input
	wrongProject.ProjectID = uuid.NewString()
	_, err = service.ConfigureInference(ctx, principal, wrongProject)
	require.ErrorIs(t, err, ErrAccessRoleMutationNotFound)
	otherCaller := principal
	otherCaller.OrganizationID = "other-org"
	_, err = service.ConfigureInference(ctx, otherCaller, input)
	require.ErrorIs(t, err, ErrAccessRoleMutationNotFound)
	enabled, err := service.ConfigureInference(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, enabled.ExactGrantPresent)
	require.Equal(t, "inference:execute", enabled.Scope)
	require.Equal(t, project.ID.String(), enabled.ProjectID)
	require.False(t, enabled.Receipt.Replayed)
	replayed, err := service.ConfigureInference(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, enabled.Receipt.ID, replayed.Receipt.ID)
	// Expired handles recover completed receipts but cannot authorize fresh writes.
	now := reads.now()
	reads.now = func() time.Time { return now.Add(SubjectReferenceTTL + time.Second) }
	replayed, err = service.ConfigureInference(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	expiredFresh := input
	expiredFresh.IdempotencyKey = "expired-fresh"
	_, err = service.ConfigureInference(ctx, principal, expiredFresh)
	require.ErrorIs(t, err, ErrAccessRoleMutationNotFound)
	changedScope := input
	changedScope.AllProjects = true
	_, err = service.ConfigureInference(ctx, principal, changedScope)
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)
	otherPrincipal := principal
	otherPrincipal.UserID = "another-user"
	_, err = service.ConfigureInference(ctx, otherPrincipal, input)
	require.Error(t, err)
	reads.now = func() time.Time { return now }
	changed := input
	changed.Enabled = false
	_, err = service.ConfigureInference(ctx, principal, changed)
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)
	changed.IdempotencyKey = "stale-disable"
	_, err = service.ConfigureInference(ctx, principal, changed)
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)
	changed.ExpectedVersion = enabled.Role.Version
	changed.IdempotencyKey = "disable-inference"
	disabled, err := service.ConfigureInference(ctx, principal, changed)
	require.NoError(t, err)
	require.False(t, disabled.ExactGrantPresent)
	// Replayed writes cannot override newer state; the exact-grant check stays live.
	replayed, err = service.ConfigureInference(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.False(t, replayed.ExactGrantPresent)
	stored, err := manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	require.Equal(t, []*accessgen.RoleGrant{baseline}, stored.Grants)

	all := input
	all.AllProjects = true
	all.ExpectedVersion = disabled.Role.Version
	all.IdempotencyKey = "enable-all-projects"
	all.Confirmed = false
	_, err = service.ConfigureInference(ctx, principal, all)
	require.ErrorIs(t, err, ErrAccessRoleMutationInvalid)
	all.Confirmed = true
	allEnabled, err := service.ConfigureInference(ctx, principal, all)
	require.NoError(t, err)
	require.True(t, allEnabled.AllProjects)
	require.True(t, allEnabled.ExactGrantPresent)
	stored, err = manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	require.True(t, hasInferenceAccessRoleGrant(stored, project.ID, true))
	require.False(t, hasInferenceAccessRoleGrant(stored, project.ID, false))
	allReplay, err := service.ConfigureInference(ctx, principal, all)
	require.NoError(t, err)
	require.True(t, allReplay.Receipt.Replayed)
	removeAll := all
	removeAll.Enabled = false
	removeAll.IdempotencyKey = "remove-all-projects"
	_, err = service.ConfigureInference(ctx, principal, removeAll)
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)
	removeAll.ExpectedVersion = allEnabled.Role.Version
	allDisabled, err := service.ConfigureInference(ctx, principal, removeAll)
	require.NoError(t, err)
	require.False(t, allDisabled.ExactGrantPresent)
	stored, err = manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
	require.NoError(t, err)
	require.Equal(t, []*accessgen.RoleGrant{baseline}, stored.Grants)

	// Existing grants may encode all projects with either no project selector
	// or an explicit wildcard. Enable converges both; disable removes both.
	for _, tc := range []struct {
		name      string
		canonical bool
		enable    bool
	}{
		{name: "enable-wildcard", canonical: false, enable: true},
		{name: "enable-both", canonical: true, enable: true},
		{name: "disable-wildcard", canonical: false, enable: false},
		{name: "disable-both", canonical: true, enable: false},
	} {
		// Cases share the same role and run sequentially to verify convergence.
		wildcard := inferenceAccessRoleGrant(project.ID, true)
		wildcard.Selectors[0].ProjectID = new("*")
		projectGrant := inferenceAccessRoleGrant(project.ID, false)
		seed := []*accessgen.RoleGrant{wildcard, projectGrant}
		if tc.canonical {
			seed = append(seed, inferenceAccessRoleGrant(project.ID, true))
		}
		tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: role manager transaction uses generated queries.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		seeded, _, err := manager.UpdateRoleTx(ctx, tx, principal.OrganizationID, organization.WorkosID.String, access.RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)}, &accessgen.UpdateRolePayload{ID: roleID, AddGrants: seed})
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		mutation := all
		mutation.Enabled = tc.enable
		mutation.IdempotencyKey = tc.name
		mutation.ExpectedVersion, err = service.roleVersion(seeded.After)
		require.NoError(t, err)
		result, err := service.ConfigureInference(ctx, principal, mutation)
		require.NoError(t, err)
		require.Equal(t, tc.enable, result.ExactGrantPresent)
		stored, err := manager.GetRoleByID(ctx, principal.OrganizationID, roleID)
		require.NoError(t, err)
		require.True(t, hasInferenceAccessRoleGrant(stored, project.ID, false), "project-specific inference remains")
		require.Contains(t, stored.Grants, baseline, "unrelated MCP scope remains")
		broad := 0
		for _, grant := range stored.Grants {
			if grant.Scope != "inference:execute" {
				continue
			}
			for _, selector := range grant.Selectors {
				if selector.ProjectID == nil {
					broad++
				} else {
					require.Equal(t, project.ID.String(), *selector.ProjectID, "wildcard spelling is removed")
				}
			}
		}
		if tc.enable {
			require.Equal(t, 1, broad, "enable keeps only one canonical grant")
			mutation.Enabled = false
			mutation.IdempotencyKey += "-cleanup"
			mutation.ExpectedVersion = result.Role.Version
			_, err = service.ConfigureInference(ctx, principal, mutation)
			require.NoError(t, err)
		} else {
			require.Zero(t, broad)
		}
	}
}
