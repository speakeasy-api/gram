package auditapi_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

var catalogPromotionActions = []audit.Action{
	audit.ActionCatalogPromotionArchive,
	audit.ActionCatalogPromotionReject,
	audit.ActionCatalogPromotionRestore,
	audit.ActionCatalogPromotionRetryDocs,
}

// seedCatalogPromotionLogs inserts rows directly, the way rows written before
// the customer feed filtered them already sit in the table.
func seedCatalogPromotionLogs(t *testing.T, ctx context.Context, ti *testInstance, authCtx *contextvalues.AuthContext, subjectID string) {
	t.Helper()

	adminSurface := string(audit.SurfaceAdmin)
	for _, action := range catalogPromotionActions {
		insertAuditLog(t, ctx, ti, auditLogSeed{
			organizationID:   authCtx.ActiveOrganizationID,
			projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			actorID:          "oidc|staff-curator",
			actorType:        "user",
			actorDisplayName: new("Staff Curator"),
			actingSurface:    &adminSurface,
			action:           string(action),
			subjectID:        subjectID,
			subjectType:      "catalog_promotion",
		})
	}
}

func seedCustomerProjectLog(t *testing.T, ctx context.Context, ti *testInstance, authCtx *contextvalues.AuthContext) uuid.UUID {
	t.Helper()

	return insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "user:alice",
		actorType:        "user",
		actorDisplayName: new("Alice"),
		action:           "project:update",
		subjectID:        uuid.NewString(),
		subjectType:      "project",
	})
}

func listPayload() *gen.ListPayload {
	return &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
		SubjectType:  nil,
		SubjectID:    nil,
	}
}

func TestAuditService_List_ExcludesCatalogPromotionEvents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	subjectID := uuid.NewString()
	seedCatalogPromotionLogs(t, ctx, ti, authCtx, subjectID)
	customerLog := seedCustomerProjectLog(t, ctx, ti, authCtx)

	result, err := ti.service.List(ctx, listPayload())
	require.NoError(t, err)
	require.Len(t, result.Logs, 1, "catalogue curation must not appear in the customer feed")
	require.Equal(t, customerLog.String(), result.Logs[0].ID)

	for _, action := range catalogPromotionActions {
		payload := listPayload()
		payload.Action = new(string(action))
		result, err := ti.service.List(ctx, payload)
		require.NoError(t, err)
		require.Empty(t, result.Logs, "an explicit %s filter must not reveal curation events", action)
	}

	bySubjectType := listPayload()
	bySubjectType.SubjectType = new("catalog_promotion")
	result, err = ti.service.List(ctx, bySubjectType)
	require.NoError(t, err)
	require.Empty(t, result.Logs, "an explicit subject type filter must not reveal curation events")

	bySubjectID := listPayload()
	bySubjectID.SubjectID = &subjectID
	result, err = ti.service.List(ctx, bySubjectID)
	require.NoError(t, err)
	require.Empty(t, result.Logs, "an explicit subject id filter must not reveal curation events")

	bySubjectIDs := listPayload()
	bySubjectIDs.SubjectIds = []string{subjectID}
	result, err = ti.service.List(ctx, bySubjectIDs)
	require.NoError(t, err)
	require.Empty(t, result.Logs, "an explicit subject id list must not reveal curation events")
}

func TestAuditService_ListFacets_ExcludesCatalogPromotionEvents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	seedCatalogPromotionLogs(t, ctx, ti, authCtx, uuid.NewString())
	seedCustomerProjectLog(t, ctx, ti, authCtx)

	result, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		ProjectSlug:  nil,
	})
	require.NoError(t, err)

	require.Len(t, result.Actions, 1, "curation actions must not appear in action facets")
	require.Equal(t, "project:update", result.Actions[0].Value)
	require.Len(t, result.Actors, 1, "curation actors must not appear in actor facets")
	require.Equal(t, "user:alice", result.Actors[0].Value)
	for _, surface := range result.Surfaces {
		require.NotEqual(t, string(audit.SurfaceAdmin), surface.Value, "curation rows must not count toward surface facets")
	}
}
