package platformmcp

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func assignmentReferenceByName(t *testing.T, options []PluginAssignmentOption, displayName string) string {
	t.Helper()
	for _, option := range options {
		if option.DisplayName == displayName {
			return option.Reference
		}
	}
	require.FailNow(t, "assignment option not found", displayName)
	return ""
}

func assignmentModesByName(options []PluginAssignmentOption) map[string]string {
	modes := make(map[string]string, len(options))
	for _, option := range options {
		modes[option.DisplayName] = option.InstallMode
	}
	return modes
}

func TestSetPluginAssignmentsSetsAndKeepsInstallModes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_install_modes")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, true)
	service := testPluginTargets(conn).WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget())
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared Tools", "shared-tools")
	now := time.Now().UTC()
	_, err = accessrepo.New(conn).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    principal.OrganizationID,
		WorkosSlug:        "engineering",
		WorkosName:        "Engineering",
		WorkosDescription: pgtype.Text{},
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: pgtype.Text{},
	})
	require.NoError(t, err)

	choices, err := service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	engineering := assignmentReferenceByName(t, choices.Assignments, "Engineering")
	everyone := assignmentReferenceByName(t, choices.Assignments, "Everyone")
	before, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)

	changed, err := service.SetPluginAssignments(ctx, principal, SetPluginAssignmentsInput{
		ProjectID: project.ID.String(), Plugin: plugin.Slug, AssignmentReferences: []string{engineering, everyone},
		InstallModes:              map[string]string{engineering: "required", everyone: "available"},
		ExpectedAssignmentVersion: before.AssignmentVersion, IdempotencyKey: "set-modes", Confirmed: true,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"required", "available"}, []string{changed.Assignments[0].InstallMode, changed.Assignments[1].InstallMode})

	read, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"Engineering": "required", "Everyone": "available"}, assignmentModesByName(read.Assignments))
	require.Equal(t, changed.AssignmentVersion, read.AssignmentVersion, "the receipt's version matches a fresh read")

	// A write that leaves modes out keeps the stored ones.
	kept, err := service.SetPluginAssignments(ctx, principal, SetPluginAssignmentsInput{
		ProjectID: project.ID.String(), Plugin: plugin.Slug, AssignmentReferences: []string{engineering, everyone},
		ExpectedAssignmentVersion: read.AssignmentVersion, IdempotencyKey: "keep-modes", Confirmed: true,
	})
	require.NoError(t, err)
	require.Equal(t, read.AssignmentVersion, kept.AssignmentVersion)
	reread, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"Engineering": "required", "Everyone": "available"}, assignmentModesByName(reread.Assignments))
}

func TestSetPluginAssignmentsConflictsAfterModeOnlyEdit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_install_mode_conflict")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, true)
	service := testPluginTargets(conn).WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget())
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared Tools", "shared-tools")
	queries := pluginsrepo.New(conn)
	assign := func(mode string) {
		t.Helper()
		_, err := queries.RemoveAllPluginAssignments(ctx, pluginsrepo.RemoveAllPluginAssignmentsParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID})
		require.NoError(t, err)
		_, err = queries.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: urn.PrincipalWildcard, InstallMode: mode})
		require.NoError(t, err)
	}
	assign("default")

	read, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Len(t, read.Assignments, 1)
	require.Equal(t, "default", read.Assignments[0].InstallMode)

	// A dashboard edit that only changes the mode must invalidate the version.
	assign("required")
	_, err = service.SetPluginAssignments(ctx, principal, SetPluginAssignmentsInput{
		ProjectID: project.ID.String(), Plugin: plugin.Slug, AssignmentReferences: []string{read.Assignments[0].Reference},
		ExpectedAssignmentVersion: read.AssignmentVersion, IdempotencyKey: "stale-mode", Confirmed: true,
	})
	require.ErrorIs(t, err, ErrPluginAssignmentMutationConflict)
}

func TestSetPluginAssignmentsRejectsConflictingModesForOneAssignment(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_install_mode_references")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, true)
	service := testPluginTargets(conn).WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget())
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared Tools", "shared-tools")

	first, err := service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	second, err := service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	everyoneA := assignmentReferenceByName(t, first.Assignments, "Everyone")
	everyoneB := assignmentReferenceByName(t, second.Assignments, "Everyone")
	require.NotEqual(t, everyoneA, everyoneB, "each read issues a fresh reference")
	read, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)

	_, err = service.SetPluginAssignments(ctx, principal, SetPluginAssignmentsInput{
		ProjectID: project.ID.String(), Plugin: plugin.Slug, AssignmentReferences: []string{everyoneA, everyoneB},
		InstallModes:              map[string]string{everyoneA: "required", everyoneB: "available"},
		ExpectedAssignmentVersion: read.AssignmentVersion, IdempotencyKey: "conflicting-references", Confirmed: true,
	})
	require.ErrorIs(t, err, ErrPluginAssignmentMutationInvalid)

	agreed, err := service.SetPluginAssignments(ctx, principal, SetPluginAssignmentsInput{
		ProjectID: project.ID.String(), Plugin: plugin.Slug, AssignmentReferences: []string{everyoneA, everyoneB},
		InstallModes:              map[string]string{everyoneA: "required", everyoneB: "required"},
		ExpectedAssignmentVersion: read.AssignmentVersion, IdempotencyKey: "agreeing-references", Confirmed: true,
	})
	require.NoError(t, err)
	require.Len(t, agreed.Assignments, 1)
	require.Equal(t, "required", agreed.Assignments[0].InstallMode)
}
