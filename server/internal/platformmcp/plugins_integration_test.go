package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginassignments "github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListPluginsPagesAProjectsPluginsWithMembershipCounts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_inventory")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)

	defaultPlugin, err := pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	marketing := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Marketing Tools", "marketing")

	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		PluginID:       marketing.ID,
		OrganizationID: principal.OrganizationID,
		PrincipalUrn:   urn.PrincipalWildcard,
	})
	require.NoError(t, err)

	// The whole project fits in one page, and every plugin in it is reported.
	page, err := service.ListPlugins(ctx, principal, ListPluginsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.Empty(t, page.NextCursor)
	require.Len(t, page.Plugins, 2)

	byID := map[string]Plugin{}
	for _, plugin := range page.Plugins {
		byID[plugin.ID] = plugin
	}
	require.True(t, byID[defaultPlugin.ID.String()].IsDefault)
	require.False(t, byID[marketing.ID.String()].IsDefault)
	require.True(t, byID[marketing.ID.String()].Assignments.AllMembers)
	require.Zero(t, byID[marketing.ID.String()].Assignments.Users)
	// The project has no package repository connected, so nothing in it can be
	// published — reported as its own state rather than as "unpublished".
	require.Equal(t, PluginPublicationNoRepository, byID[marketing.ID.String()].Publication)

	// A one-per-page walk covers the same plugins exactly once.
	seen := map[string]bool{}
	cursor := ""
	for range 3 {
		step, err := service.ListPlugins(ctx, principal, ListPluginsInput{ProjectID: project.ID.String(), Limit: 1, Cursor: cursor})
		require.NoError(t, err)
		for _, plugin := range step.Plugins {
			require.False(t, seen[plugin.ID], "plugin returned twice")
			seen[plugin.ID] = true
		}
		cursor = step.NextCursor
		if cursor == "" {
			break
		}
	}
	require.Len(t, seen, 2)
}

func TestListPluginAssignmentsReturnsOpaqueProjectBoundReferences(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_assignments")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	result, err := service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.Equal(t, project.ID.String(), result.ProjectID)
	require.Equal(t, now.Add(SubjectReferenceTTL).Format(time.RFC3339), result.ReferencesExpireAt)
	require.NotEmpty(t, result.Assignments)
	require.Equal(t, PluginAssignmentOption{Kind: "everyone", DisplayName: "Everyone", Reference: result.Assignments[0].Reference}, result.Assignments[0])

	resolved, err := service.assignmentReferences.DecodeScoped(result.Assignments[0].Reference, principal, subjectKindPluginAssignment, project.ID.String(), now)
	require.NoError(t, err)
	require.Equal(t, urn.PrincipalWildcard, resolved)

	_, otherProject := seedRegistrationLifecycle(t, ctx, conn)
	_, err = service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: otherProject.ID.String()})
	require.ErrorIs(t, err, ErrPluginProjectNotFound)
	_, err = service.assignmentReferences.DecodeScoped(result.Assignments[0].Reference, principal, subjectKindPluginAssignment, otherProject.ID.String(), now)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

func TestPluginReadsIgnoreCrossTenantAssignmentRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_assignment_tenancy")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared Tools", "shared")

	before, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Zero(t, before.Plugin.Assignments.Roles)

	foreignPrincipal, _ := seedRegistrationLifecycle(t, ctx, conn)
	require.NoError(t, testrepo.New(conn).InsertPluginAssignmentFixture(ctx, testrepo.InsertPluginAssignmentFixtureParams{
		PluginID:       plugin.ID,
		OrganizationID: foreignPrincipal.OrganizationID,
		PrincipalUrn:   "role:organization:" + uuid.NewString(),
	}))

	after, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, before.AssignmentVersion, after.AssignmentVersion)
	require.Zero(t, after.Plugin.Assignments.Roles)
	require.Empty(t, after.Assignments)
	require.True(t, after.AssignmentDetailsComplete)
}

func TestListPluginAssignmentsBoundsResolverWorkInSQL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_assignment_bound")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	now := time.Now().UTC()
	var beyondBoundRole string
	for index := range maxPluginMembers + 1 {
		role, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
			OrganizationID:    principal.OrganizationID,
			WorkosSlug:        fmt.Sprintf("role-%03d", index),
			WorkosName:        fmt.Sprintf("Role %03d", index),
			WorkosDescription: pgtype.Text{},
			WorkosCreatedAt:   conv.ToPGTimestamptz(now),
			WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
			WorkosLastEventID: pgtype.Text{},
		})
		require.NoError(t, err)
		if index == maxPluginMembers {
			beyondBoundRole = role.RoleUrn
		}
	}

	service := testPluginTargets(conn)
	result, err := service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Len(t, result.Assignments, maxPluginMembers)
	require.Equal(t, "everyone", result.Assignments[0].Kind)

	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Beyond Bound", "beyond-bound")
	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: beyondBoundRole})
	require.NoError(t, err)
	detail, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.True(t, detail.AssignmentDetailsComplete)
	require.False(t, detail.AssignmentsTruncated)
	require.Len(t, detail.Assignments, 1)
	require.Equal(t, "Role 100", detail.Assignments[0].DisplayName)
}

func TestPluginAssignmentOptionsUseCanonicalRoleAndLongAttributePrincipals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_assignment_principals")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	now := time.Now().UTC()
	role, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID: principal.OrganizationID, WorkosSlug: "custom-role", WorkosName: "Custom Role",
		WorkosDescription: pgtype.Text{}, WorkosCreatedAt: conv.ToPGTimestamptz(now), WorkosUpdatedAt: conv.ToPGTimestamptz(now), WorkosLastEventID: pgtype.Text{},
	})
	require.NoError(t, err)
	longValue := strings.Repeat("long-attribute-value-", 8)
	_, err = directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID: principal.OrganizationID, UserID: pgtype.Text{}, WorkosDirectoryUserID: "directory-user-" + uuid.NewString(),
		Email: conv.ToPGText("member@example.test"), Attributes: []byte(`{"department_name":"` + longValue + `","manager_email":"private@example.test"}`),
		WorkosCreatedAt: conv.ToPGTimestamptz(now), WorkosUpdatedAt: conv.ToPGTimestamptz(now), WorkosLastEventID: pgtype.Text{}, RestoreDeleted: true,
	})
	require.NoError(t, err)

	service := testPluginTargets(conn)
	result, err := service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	byName := map[string]PluginAssignmentOption{}
	for _, assignment := range result.Assignments {
		byName[assignment.DisplayName] = assignment
	}
	require.NotContains(t, byName, "manager_email: private@example.test")
	for displayName, expectedURN := range map[string]string{
		"Custom Role":                   role.RoleUrn,
		"department_name: " + longValue: directory.AttributePrincipal("department_name", longValue),
	} {
		assignment, ok := byName[displayName]
		require.True(t, ok, displayName)
		resolved, err := service.assignmentReferences.DecodeScoped(assignment.Reference, principal, subjectKindPluginAssignment, project.ID.String(), service.now().UTC())
		require.NoError(t, err)
		require.Equal(t, expectedURN, resolved)
	}

	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Sensitive Attribute", "sensitive-attribute")
	sensitiveURN := directory.AttributePrincipal("manager_email", "private@example.test")
	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: sensitiveURN})
	require.NoError(t, err)
	detail, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.False(t, detail.AssignmentDetailsComplete)
	require.Empty(t, detail.Assignments)
}

func TestGetPluginAssignmentVersionChangesAfterDashboardStyleEdit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_assignment_version")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared Tools", "shared")

	before, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.NotEmpty(t, before.AssignmentVersion)
	require.Empty(t, before.Assignments)
	require.True(t, before.AssignmentDetailsComplete)

	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		PluginID:       plugin.ID,
		OrganizationID: principal.OrganizationID,
		PrincipalUrn:   urn.PrincipalWildcard,
	})
	require.NoError(t, err)

	after, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.NotEqual(t, before.AssignmentVersion, after.AssignmentVersion)
	require.Len(t, after.Assignments, 1)
	require.Equal(t, "everyone", after.Assignments[0].Kind)
	require.True(t, after.AssignmentDetailsComplete)
}

func TestGetPluginResolvesAnExactTargetAndReportsMembership(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_membership")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	marketing := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Marketing Tools", "marketing")

	// Named by slug, by exact name, and by id: one plugin, three ways to say it.
	for _, target := range []string{"marketing", "Marketing Tools", "MARKETING TOOLS", marketing.ID.String()} {
		got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: target})
		require.NoError(t, err, target)
		require.Equal(t, marketing.ID.String(), got.Plugin.ID)
		require.Empty(t, got.Servers)
		require.Empty(t, got.Skills)
		require.False(t, got.Truncated)
	}
}

func TestGetPluginRefusesAnUnmatchedTargetRatherThanFallingBackToDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_refusal")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	_, err = pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)

	_, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "marketing"})
	require.ErrorIs(t, err, ErrPluginNotFound)

	// Two plugins sharing a name is ambiguous, never resolved by picking one.
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared", "shared-one")
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared", "shared-two")
	_, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "Shared"})
	require.ErrorIs(t, err, ErrPluginAmbiguous)
}

func TestPluginInventoryRefusesAnotherOrganizationsProject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_tenancy")
	require.NoError(t, err)

	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	_, otherProject := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, otherProject.ID, "Foreign", "foreign")

	_, err = service.ListPlugins(ctx, principal, ListPluginsInput{ProjectID: otherProject.ID.String()})
	require.ErrorIs(t, err, ErrPluginProjectNotFound)

	_, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: otherProject.ID.String(), Plugin: "foreign"})
	require.ErrorIs(t, err, ErrPluginProjectNotFound)

	_, err = service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: otherProject.ID.String()})
	require.ErrorIs(t, err, ErrPluginProjectNotFound)
}

func TestSetPluginAssignmentsReplacesAtomicallyAndReplaysSafely(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_set_plugin_assignments")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, true)
	service := testPluginTargets(conn).WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget())
	_, err = service.SetPluginAssignments(ctx, principal, SetPluginAssignmentsInput{
		ProjectID:                 project.ID.String(),
		Plugin:                    uuid.Nil.String(),
		ExpectedAssignmentVersion: "opaque-version",
		IdempotencyKey:            "reject-zero-plugin-id",
		Confirmed:                 true,
	})
	require.ErrorIs(t, err, ErrPluginAssignmentMutationInvalid)

	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared Tools", "shared-tools")
	now := time.Now().UTC()
	role, err := accessrepo.New(conn).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    principal.OrganizationID,
		WorkosSlug:        "engineering",
		WorkosName:        "Engineering",
		WorkosDescription: pgtype.Text{},
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: pgtype.Text{},
	})
	require.NoError(t, err)

	before, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	choices, err := service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	var reference string
	for _, assignment := range choices.Assignments {
		if assignment.DisplayName == role.WorkosName {
			reference = assignment.Reference
		}
	}
	require.NotEmpty(t, reference)

	input := SetPluginAssignmentsInput{
		ProjectID: project.ID.String(), Plugin: plugin.Slug, AssignmentReferences: []string{reference},
		ExpectedAssignmentVersion: before.AssignmentVersion, IdempotencyKey: "set-assignments", Confirmed: true,
	}
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	changed, err := service.SetPluginAssignments(ctx, principal, input)
	require.NoError(t, err)
	require.False(t, changed.Receipt.Replayed)
	require.Equal(t, PluginPublicationNoRepository, changed.Plugin.Publication)
	require.Equal(t, PluginAssignmentSummary{Roles: 1}, changed.Plugin.Assignments)
	zeroMembers := NewSubjectCount(0)
	require.Equal(t, []PluginAssignmentSummaryResult{{Kind: "role", DisplayName: "Engineering", MemberCount: &zeroMembers}}, changed.Assignments)
	require.NotEqual(t, before.AssignmentVersion, changed.AssignmentVersion)

	stored, err := pluginsrepo.New(conn).ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, role.RoleUrn, stored[0].PrincipalUrn)

	conflicting := input
	conflicting.AssignmentReferences = nil
	_, err = service.SetPluginAssignments(ctx, principal, conflicting)
	require.ErrorIs(t, err, ErrPluginAssignmentMutationConflict)

	stale := input
	stale.IdempotencyKey = "stale-version"
	_, err = service.SetPluginAssignments(ctx, principal, stale)
	require.ErrorIs(t, err, ErrPluginAssignmentMutationConflict)

	_, err = pluginsrepo.New(conn).UpdatePlugin(ctx, pluginsrepo.UpdatePluginParams{
		Name: "Renamed Tools", Slug: "renamed-tools", Description: plugin.Description,
		ID: plugin.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID,
	})
	require.NoError(t, err)
	replayed, err := service.SetPluginAssignments(ctx, principal, input)
	require.NoError(t, err, "an exact completed receipt replays even when its slug no longer resolves")
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, changed.Receipt.ID, replayed.Receipt.ID)
	require.Equal(t, changed.Plugin, replayed.Plugin)
	afterAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	require.Equal(t, beforeAudit+1, afterAudit, "receipt replay must not write a second audit event")
	stored, err = pluginsrepo.New(conn).ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, stored, 1, "stale writes leave the committed assignment untouched")

	receipt, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		Operation:      operationSetPluginAssignments,
		IdempotencyKey: input.IdempotencyKey,
		UserID:         conv.ToPGText(principal.UserID),
		SubjectUrn:     userSubjectURN(principal.UserID),
	})
	require.NoError(t, err)
	var safe SetPluginAssignmentsReceiptResult
	require.NoError(t, json.Unmarshal(receipt.ResultPayload, &safe))
	require.NotContains(t, string(receipt.ResultPayload), role.RoleUrn)
	require.NotContains(t, string(receipt.ResultPayload), reference)
}

func TestPluginAssignmentMutationReceiptRollsBackDomainAndReceipt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_audience_receipt_rollback")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Rollback", "rollback")
	store := NewPluginAssignmentMutationReceiptStore(conn)
	normalized := normalizedPluginAssignmentMutationInput(project.ID, plugin.ID.String(), nil, "version")
	_, err = store.Execute(ctx, principal, project, "rollback", normalized, func(ctx context.Context, tx pgx.Tx) (SetPluginAssignmentsReceiptResult, error) {
		_, err := pluginsrepo.New(tx).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
			PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: urn.PrincipalWildcard,
		})
		require.NoError(t, err)
		return SetPluginAssignmentsReceiptResult{}, errors.New("injected audit failure")
	})
	require.ErrorContains(t, err, "injected audit failure")

	assignments, err := pluginsrepo.New(conn).ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Empty(t, assignments)
	_, err = platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, Operation: operationSetPluginAssignments,
		IdempotencyKey: "rollback", UserID: conv.ToPGText(principal.UserID), SubjectUrn: userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestConcurrentVersionProtectedAssignmentWritesSerialize(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_audience_race")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Race", "race")
	versionKey := []byte("race-version-key")
	expected := pluginAssignmentVersion(versionKey, project.ID, plugin.ID, nil)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)
	write := func(principalURN string) {
		defer wg.Done()
		<-start
		tx, beginErr := conn.Begin(ctx) //nolint:glint // transaction contains only package APIs and SQLc-generated queries
		if beginErr != nil {
			results <- beginErr
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		locked, lockErr := pluginassignments.Lock(ctx, tx, principal.OrganizationID, project.ID, plugin.ID)
		if lockErr != nil {
			results <- lockErr
			return
		}
		_, replaceErr := pluginassignments.Replace(ctx, tx, audit.NewLogger(), locked, pluginassignments.Input{
			OrganizationID: principal.OrganizationID, ProjectID: project.ID, PluginID: plugin.ID,
			PrincipalURNs: []string{principalURN}, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		}, func(_ context.Context, _ pluginsrepo.Plugin, current, _ []string) error {
			if pluginAssignmentVersion(versionKey, project.ID, plugin.ID, current) != expected {
				return ErrPluginAssignmentMutationConflict
			}
			return nil
		})
		if replaceErr != nil {
			results <- replaceErr
			return
		}
		results <- tx.Commit(ctx)
	}
	go write(urn.PrincipalWildcard)
	go write("email:member@example.com")
	close(start)
	wg.Wait()
	close(results)

	succeeded, conflicted := 0, 0
	for result := range results {
		switch {
		case result == nil:
			succeeded++
		case errors.Is(result, ErrPluginAssignmentMutationConflict):
			conflicted++
		default:
			require.NoError(t, result)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicted)
	assignments, err := pluginsrepo.New(conn).ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestSetPluginAssignmentsRefusesHiddenCurrentAssignments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_set_hidden_plugin_audience")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, true)
	service := testPluginTargets(conn).WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget())
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Hidden Audience", "hidden-audience")
	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: "user:private-user-id",
	})
	require.NoError(t, err)
	before, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.False(t, before.AssignmentDetailsComplete)

	_, err = service.SetPluginAssignments(ctx, principal, SetPluginAssignmentsInput{
		ProjectID: project.ID.String(), Plugin: plugin.ID.String(), AssignmentReferences: nil,
		ExpectedAssignmentVersion: before.AssignmentVersion, IdempotencyKey: "hidden-audience", Confirmed: true,
	})
	require.ErrorIs(t, err, ErrPluginAssignmentMutationInvalid)
	stored, err := pluginsrepo.New(conn).ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, "user:private-user-id", stored[0].PrincipalUrn)
}

func TestSetPluginAssignmentsRejectsExpiredAndCrossProjectReferences(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_set_plugin_assignment_references")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, true)
	service := testPluginTargets(conn).WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget())
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared Tools", "shared-tools")
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	before, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	choices, err := service.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)

	_, otherProject := seedRegistrationLifecycle(t, ctx, conn)
	crossProject, err := service.assignmentReferences.EncodeScoped(principal, subjectKindPluginAssignment, otherProject.ID.String(), urn.PrincipalWildcard, now)
	require.NoError(t, err)
	for _, test := range []struct {
		name      string
		reference string
		at        time.Time
	}{
		{name: "cross-project", reference: crossProject, at: now},
		{name: "expired", reference: choices.Assignments[0].Reference, at: now.Add(SubjectReferenceTTL)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			localService := testPluginTargets(conn).WithAssignmentMutations(flags, NewPostgresOrganizationSlugResolver(conn), audit.NewLogger(), testOperationBudget())
			localService.now = func() time.Time { return test.at }
			_, err := localService.SetPluginAssignments(ctx, principal, SetPluginAssignmentsInput{
				ProjectID: project.ID.String(), Plugin: plugin.ID.String(), AssignmentReferences: []string{test.reference},
				ExpectedAssignmentVersion: before.AssignmentVersion, IdempotencyKey: test.name, Confirmed: true,
			})
			require.ErrorIs(t, err, ErrPluginAssignmentMutationNotFound)
		})
	}
}

func seedPlugin(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, name, slug string) pluginsrepo.Plugin {
	t.Helper()

	plugin, err := pluginsrepo.New(conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           name,
		Slug:           slug,
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)
	return plugin
}
