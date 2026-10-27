package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/directoryreview"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// startDirectoryReview records the organization's review baseline, as the
// first sweep does, so sources seen after it are raised as new.
func startDirectoryReview(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) {
	t.Helper()
	_, err := directoryreview.ReconcileOrganization(ctx, conn, orgID)
	require.NoError(t, err)
}

func reviewNewGroup(t *testing.T, ctx context.Context, ti *testInstance, orgID, name string) uuid.UUID {
	t.Helper()
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, name)
	_, err := directoryreview.ReconcileGroup(ctx, ti.conn, orgID, groupID)
	require.NoError(t, err)
	return groupID
}

func onlyReviewItem(t *testing.T, ctx context.Context, ti *testInstance) *gen.DirectoryReviewItem {
	t.Helper()
	listed, err := ti.service.ListDirectoryReviewItems(ctx, &gen.ListDirectoryReviewItemsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	return listed.Items[0]
}

func reviewItemState(t *testing.T, ctx context.Context, conn *pgxpool.Pool, id string) string {
	t.Helper()
	state, err := testrepo.New(conn).GetDirectoryReviewItemStateFixture(ctx, uuid.MustParse(id))
	require.NoError(t, err)
	return state
}

func TestService_ListDirectoryReviewItems_ProposesRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID

	listed, err := ti.service.ListDirectoryReviewItems(ctx, &gen.ListDirectoryReviewItemsPayload{})
	require.NoError(t, err)
	require.Empty(t, listed.Items)

	startDirectoryReview(t, ctx, ti.conn, orgID)
	reviewNewGroup(t, ctx, ti, orgID, "Platform")

	item := onlyReviewItem(t, ctx, ti)
	require.Equal(t, directoryreview.KindNewGroup, item.Kind)
	require.Equal(t, int32(1), item.Occurrence)
	require.Equal(t, "Platform", item.Item.SourceName)
	require.Empty(t, item.Item.ExistingRoleUrns)
	require.Len(t, item.Item.ProposedRoles, 1)
	require.Equal(t, directoryRoleSetupActionCreate, item.Item.ProposedRoles[0].RoleAction)

	// The version matches bulk setup's for the same source.
	preview, err := ti.service.PreviewDirectoryRoleSetup(ctx, &gen.PreviewDirectoryRoleSetupPayload{})
	require.NoError(t, err)
	require.Equal(t, previewItemByName(t, preview, "Platform").Version, item.Item.Version)
}

func TestService_ResolveDirectoryReviewItem_ApproveCreatesRoleAndMapping(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	startDirectoryReview(t, ctx, ti.conn, orgID)
	reviewNewGroup(t, ctx, ti, orgID, "Platform")
	item := onlyReviewItem(t, ctx, ti)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryReviewItemApprove)
	require.NoError(t, err)
	expectWorkOSRoleCreate(ti, "Platform")

	createName := "Platform"
	result, err := ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{
		ID: item.ID, Decision: directoryReviewDecisionApprove, Version: &item.Item.Version,
		Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &createName}},
	})
	require.NoError(t, err)
	require.Equal(t, directoryreview.StateApproved, result.State)
	require.Len(t, result.CreatedRoles, 1)
	require.Len(t, result.Mappings, 1)
	require.Equal(t, result.CreatedRoles[0].PrincipalUrn, result.Mappings[0].RoleUrn)
	require.Len(t, result.SideEffects, 1)

	require.Equal(t, directoryreview.StateApproved, reviewItemState(t, ctx, ti.conn, item.ID), "approved, not superseded by its own mapping write")
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryReviewItemApprove)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	resolvedWebhooks, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.DirectoryReviewResolvedV1.EventType()))
	require.NoError(t, err)
	require.EqualValues(t, 1, resolvedWebhooks, "the decision is announced once")

	// Resolving it again is a conflict, even when the stale request would
	// also fail validation.
	_, err = ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{ID: item.ID, Decision: directoryReviewDecisionDismiss})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{ID: item.ID, Decision: directoryReviewDecisionApprove})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestService_DirectoryReviewSettings_TogglesEmails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)

	settings, err := ti.service.GetDirectoryReviewSettings(ctx, &gen.GetDirectoryReviewSettingsPayload{})
	require.NoError(t, err)
	require.True(t, settings.EmailsEnabled, "emails are on by default")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryReviewSettingsUpdate)
	require.NoError(t, err)

	settings, err = ti.service.UpdateDirectoryReviewSettings(ctx, &gen.UpdateDirectoryReviewSettingsPayload{EmailsEnabled: false})
	require.NoError(t, err)
	require.False(t, settings.EmailsEnabled)
	settings, err = ti.service.GetDirectoryReviewSettings(ctx, &gen.GetDirectoryReviewSettingsPayload{})
	require.NoError(t, err)
	require.False(t, settings.EmailsEnabled)

	// Repeating the same setting changes and records nothing.
	_, err = ti.service.UpdateDirectoryReviewSettings(ctx, &gen.UpdateDirectoryReviewSettingsPayload{EmailsEnabled: false})
	require.NoError(t, err)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryReviewSettingsUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryReviewSettingsUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, true, beforeSnapshot["emails_enabled"])
	require.Equal(t, false, afterSnapshot["emails_enabled"])

	settings, err = ti.service.UpdateDirectoryReviewSettings(ctx, &gen.UpdateDirectoryReviewSettingsPayload{EmailsEnabled: true})
	require.NoError(t, err)
	require.True(t, settings.EmailsEnabled)
}

func TestService_ResolveDirectoryReviewItem_StaleVersionChangesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()
	startDirectoryReview(t, ctx, ti.conn, orgID)
	reviewNewGroup(t, ctx, ti, orgID, "Platform")
	item := onlyReviewItem(t, ctx, ti)

	stale := "stale"
	_, err := ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{
		ID: item.ID, Decision: directoryReviewDecisionApprove, Version: &stale,
		Roles: []*gen.ApplyDirectoryRoleSetupRole{{RoleUrn: &viewer}},
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, directoryreview.StatePending, reviewItemState(t, ctx, ti.conn, item.ID), "the rolled-back decision leaves the item pending")
}

func TestService_ResolveDirectoryReviewItem_Dismiss(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	startDirectoryReview(t, ctx, ti.conn, orgID)
	reviewNewGroup(t, ctx, ti, orgID, "Platform")
	item := onlyReviewItem(t, ctx, ti)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryReviewItemDismiss)
	require.NoError(t, err)

	result, err := ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{ID: item.ID, Decision: directoryReviewDecisionDismiss})
	require.NoError(t, err)
	require.Equal(t, directoryreview.StateDismissed, result.State)
	require.Empty(t, result.Mappings)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryReviewItemDismiss)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	listed, err := ti.service.ListDirectoryReviewItems(ctx, &gen.ListDirectoryReviewItemsPayload{})
	require.NoError(t, err)
	require.Empty(t, listed.Items)
}

func TestService_ResolveDirectoryReviewItem_OptOutKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()

	// department has a mapping, so a new department value is a decision.
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "eng@test.com", `{"department":"Engineering"}`)
	key, value := "department", "Engineering"
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind: directoryRoleMappingSourceAttribute, AttributeKey: &key, AttributeValue: &value, RoleUrns: []string{viewer},
	})
	require.NoError(t, err)
	startDirectoryReview(t, ctx, ti.conn, orgID)

	_, workosUserID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "sales@test.com", `{"department":"Sales"}`)
	_, err = directoryreview.ReconcileUserChange(ctx, ti.conn, orgID, workosUserID, nil, directoryreview.UserAttributes{"department": "Sales"})
	require.NoError(t, err)
	item := onlyReviewItem(t, ctx, ti)
	require.Equal(t, directoryreview.KindNewAttributeValue, item.Kind)

	_, err = ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{ID: item.ID, Decision: directoryReviewDecisionOptOutKey})
	require.NoError(t, err)
	require.Equal(t, directoryreview.StateDismissed, reviewItemState(t, ctx, ti.conn, item.ID))

	optedOut, err := testrepo.New(ti.conn).DirectoryReviewAttributeOptedOutFixture(ctx, testrepo.DirectoryReviewAttributeOptedOutFixtureParams{
		OrganizationID: orgID, AttributeKey: "department",
	})
	require.NoError(t, err)
	require.True(t, optedOut)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryReviewItemOptOut)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestService_ResolveDirectoryReviewItem_ApproveMissingSourceRemovesMappings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()

	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Platform")
	id := groupID.String()
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &id, RoleUrns: []string{viewer},
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(ti.conn).SetDirectoryGroupWorkOSDeletedFixture(ctx, testrepo.SetDirectoryGroupWorkOSDeletedFixtureParams{Deleted: true, ID: groupID}))
	startDirectoryReview(t, ctx, ti.conn, orgID)

	item := onlyReviewItem(t, ctx, ti)
	require.Equal(t, directoryreview.KindSourceMissing, item.Kind)
	require.Equal(t, int64(0), item.Item.DirectoryPeople)
	require.Equal(t, []string{viewer}, item.Item.ExistingRoleUrns)

	// Roles are refused: approving a missing source only removes.
	_, err = ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{
		ID: item.ID, Decision: directoryReviewDecisionApprove, Version: &item.Item.Version,
		Roles: []*gen.ApplyDirectoryRoleSetupRole{{RoleUrn: &viewer}},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	result, err := ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{
		ID: item.ID, Decision: directoryReviewDecisionApprove, Version: &item.Item.Version,
	})
	require.NoError(t, err)
	require.Equal(t, directoryreview.StateApproved, result.State)
	require.Empty(t, result.Mappings)

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Empty(t, listed.Mappings)
}

func TestService_ResolveDirectoryReviewItem_RejectsMismatchedDecisions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	startDirectoryReview(t, ctx, ti.conn, orgID)
	reviewNewGroup(t, ctx, ti, orgID, "Platform")
	item := onlyReviewItem(t, ctx, ti)
	createName := "Platform"

	for name, payload := range map[string]*gen.ResolveDirectoryReviewItemPayload{
		"approve without version": {ID: item.ID, Decision: directoryReviewDecisionApprove, Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &createName}}},
		"approve without roles":   {ID: item.ID, Decision: directoryReviewDecisionApprove, Version: &item.Item.Version},
		"dismiss with roles":      {ID: item.ID, Decision: directoryReviewDecisionDismiss, Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &createName}}},
		"opt out of a group":      {ID: item.ID, Decision: directoryReviewDecisionOptOutKey},
		"role with both fields":   {ID: item.ID, Decision: directoryReviewDecisionDismiss, Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &createName, RoleUrn: &createName}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.ResolveDirectoryReviewItem(ctx, payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}

	_, err := ti.service.ResolveDirectoryReviewItem(ctx, &gen.ResolveDirectoryReviewItemPayload{ID: uuid.NewString(), Decision: directoryReviewDecisionDismiss})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Equal(t, directoryreview.StatePending, reviewItemState(t, ctx, ti.conn, item.ID))
}
