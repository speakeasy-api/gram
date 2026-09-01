package access

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// withUniqueDetectionOrg rewrites the auth context onto a per-test org id and
// grants it org admin. The test ClickHouse database is shared across the
// package's parallel tests while the mock auth context always carries the
// same org id, so org-scoped detection reads would otherwise observe sibling
// tests' writes.
func withUniqueDetectionOrg(t *testing.T, ctx context.Context, ti *testInstance) (context.Context, string, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *authCtx
	clone.ActiveOrganizationID = "detections-test-org-" + uuid.NewString()
	ctx = contextvalues.SetAuthContext(ctx, &clone)

	seedOrganization(t, ctx, ti.conn, clone.ActiveOrganizationID)
	email := "member@example.com"
	if clone.Email != nil {
		email = *clone.Email
	}
	workosUserID := "workos-user-" + uuid.NewString()
	membershipID := "membership-" + uuid.NewString()
	seedConnectedUser(t, ctx, ti.conn, clone.ActiveOrganizationID, clone.UserID, email, "Detection Test User", workosUserID, membershipID)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, clone.ActiveOrganizationID))
	seedRoleAssignment(t, ctx, ti.conn, clone.ActiveOrganizationID, clone.UserID, mockMember("", membershipID, workosUserID, authz.SystemRoleAdmin))

	// Retain the prepared grant so revocation tests prove it cannot override
	// live membership and grant state.
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, clone.ActiveOrganizationID)})
	return ctx, clone.ActiveOrganizationID, workosUserID
}

func seedAIDetection(t *testing.T, ctx context.Context, ti *testInstance, orgID, targetID, serial, email, signal, category string, seenAt time.Time) {
	t.Helper()
	require.NoError(t, telemetryrepo.New(ti.chConn).UpsertAIDetections(ctx, []telemetryrepo.UpsertAIDetectionParams{{
		OrganizationID: orgID,
		TargetID:       targetID,
		DeviceSerial:   serial,
		UserEmail:      email,
		Signal:         signal,
		Category:       category,
		Version:        "",
		SeenAt:         seenAt,
		UpdatedAt:      time.Now().UTC(),
	}}))
}

func seedAIDetectionDirectoryGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, memberEmails []string) uuid.UUID {
	t.Helper()
	// directory_groups carries an organizations FK, so the per-test org must
	// exist before its directory rows do.
	seedOrganization(t, ctx, conn, orgID)
	now := time.Now().UTC()
	repo := directoryrepo.New(conn)

	workosGroupID := "grp_" + uuid.NewString()
	groupID, err := repo.UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         orgID,
		WorkosDirectoryGroupID: workosGroupID,
		Name:                   "Data Science",
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(now),
		WorkosLastEventID:      conv.ToPGText("event_" + workosGroupID),
	})
	require.NoError(t, err)

	for _, email := range memberEmails {
		workosUserID := "du_" + uuid.NewString()
		userID, err := repo.UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
			OrganizationID:        orgID,
			UserID:                conv.ToPGTextEmpty(""),
			WorkosDirectoryUserID: workosUserID,
			Email:                 conv.ToPGText(email),
			Attributes:            []byte(`{}`),
			RestoreDeleted:        true,
			WorkosCreatedAt:       conv.ToPGTimestamptz(now),
			WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
			WorkosLastEventID:     conv.ToPGText("event_" + workosUserID),
		})
		require.NoError(t, err)

		_, err = repo.OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
			DirectoryUserID:        userID,
			DirectoryGroupID:       groupID,
			WorkosDirectoryUserID:  workosUserID,
			WorkosDirectoryGroupID: workosGroupID,
			WorkosCreatedAt:        conv.ToPGTimestamptz(now),
		})
		require.NoError(t, err)
	}

	return groupID
}

func TestService_ListAIDetections_AggregatesAndDecoratesFromCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)

	now := time.Now().UTC().Truncate(time.Second)
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-1", "alex@example.com", "installed", "harness", now.Add(-2*time.Hour))
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-2", "sam@example.com", "installed", "harness", now.Add(-time.Hour))
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-2", "sam@example.com", "running", "harness", now)
	// An id the catalog does not know: an agent shipping a newer compiled-in
	// list than this server. It must surface under its raw id.
	seedAIDetection(t, ctx, ti, orgID, "brand-new-tool", "serial-1", "alex@example.com", "installed", "local_model", now)
	// Another org's detections must never leak into this org's inventory.
	seedAIDetection(t, ctx, ti, "detections-test-org-"+uuid.NewString(), "ollama", "serial-9", "eve@example.com", "running", "local_model", now)

	result, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Detections, 2)

	byTarget := map[string]*gen.AIDetection{}
	for _, detection := range result.Detections {
		byTarget[detection.TargetID] = detection
	}

	cursor := byTarget["cursor"]
	require.NotNil(t, cursor)
	require.Equal(t, "Cursor", cursor.DisplayName, "known ids are decorated from the catalog")
	require.Equal(t, "harness", cursor.Category)
	require.EqualValues(t, 2, cursor.UserCount)
	require.EqualValues(t, 2, cursor.DeviceCount)
	require.ElementsMatch(t, []string{"installed", "running"}, cursor.Signals)
	require.Equal(t, now.Add(-2*time.Hour).Format(time.RFC3339), cursor.FirstSeen)
	require.Equal(t, now.Format(time.RFC3339), cursor.LastSeen)

	unknown := byTarget["brand-new-tool"]
	require.NotNil(t, unknown, "ids the catalog does not know must still be listed")
	require.Equal(t, "brand-new-tool", unknown.DisplayName, "unknown ids echo the raw target id")
	require.Equal(t, "local_model", unknown.Category, "unknown ids keep the category recorded at detection time")
}

func TestService_ListAIDetections_RejectsStaleGrantWithoutLiveMembership(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *authCtx
	clone.ActiveOrganizationID = "detections-test-org-" + uuid.NewString()
	seedOrganization(t, ctx, ti.conn, clone.ActiveOrganizationID)
	ctx = contextvalues.SetAuthContext(ctx, &clone)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, clone.ActiveOrganizationID)})

	_, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}

func TestService_ListAIDetections_RejectsStaleGrantAfterLiveRoleRevocation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, workosUserID := withUniqueDetectionOrg(t, ctx, ti)

	deleted, err := accessrepo.New(ti.conn).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{
		OrganizationID: orgID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	_, err = ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}

func TestService_ListAIDetections_AllowsValidatedSupportSessionWithPreparedOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	supportOrgID := "detections-test-org-" + uuid.NewString()
	seedOrganization(t, ctx, ti.conn, supportOrgID)
	authCtx.ActiveOrganizationID = supportOrgID
	authCtx.IsAdmin = true
	authCtx.SupportOrganizationID = supportOrgID
	ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, supportOrgID)})

	seedAIDetection(t, ctx, ti, supportOrgID, "cursor", "serial-1", "member@example.com", "installed", "harness", time.Now().UTC())

	result, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Detections, 1)
	require.Equal(t, "cursor", result.Detections[0].TargetID)
}

func TestService_ListAIDetections_FiltersByCategory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)

	now := time.Now().UTC()
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-1", "alex@example.com", "installed", "harness", now)
	seedAIDetection(t, ctx, ti, orgID, "ollama", "serial-1", "alex@example.com", "running", "local_model", now)

	result, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: new("local_model"), DirectoryGroupID: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Detections, 1)
	require.Equal(t, "ollama", result.Detections[0].TargetID)
}

func TestService_ListAIDetections_FiltersByDirectoryGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)

	now := time.Now().UTC()
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-1", "member@example.com", "installed", "harness", now)
	seedAIDetection(t, ctx, ti, orgID, "ollama", "serial-2", "outsider@example.com", "running", "local_model", now)

	groupID := seedAIDetectionDirectoryGroup(t, ctx, ti.conn, orgID, []string{"Member@example.com"})
	emptyGroupID := seedAIDetectionDirectoryGroup(t, ctx, ti.conn, orgID, nil)

	result, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: new(groupID.String()), SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Detections, 1)
	require.Equal(t, "cursor", result.Detections[0].TargetID, "the team filter scopes detections to the group's member emails")

	empty, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: new(emptyGroupID.String()), SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, empty.Detections, "a group with no active members matches nothing")

	_, err = ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: new("not-a-uuid"), SessionToken: nil})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeBadRequest, shareableErr.Code)
}
