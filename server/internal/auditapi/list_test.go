package auditapi_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

type auditLogSeed struct {
	organizationID     string
	projectID          uuid.NullUUID
	actorID            string
	actorType          string
	actorDisplayName   *string
	actorSlug          *string
	action             string
	subjectID          string
	subjectType        string
	subjectDisplayName *string
	subjectSlug        *string
	beforeSnapshot     []byte
	afterSnapshot      []byte
	metadata           []byte
}

func TestAuditService_List_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAuditService(t)

	_, err := ti.service.List(t.Context(), &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestAuditService_List_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Logs)
	require.Nil(t, result.NextCursor)
}

func TestAuditService_List_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	firstBefore := json.RawMessage(`{"name":"before-first"}`)
	firstAfter := json.RawMessage(`{"name":"after-first"}`)
	secondMetadata := []byte(`{"ip":"127.0.0.1","source":"ui"}`)

	insertedFirst := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:     authCtx.ActiveOrganizationID,
		projectID:          uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:            "user:first",
		actorType:          "user",
		actorDisplayName:   new("First User"),
		actorSlug:          new("first-user"),
		action:             "project:update",
		subjectID:          "project-1",
		subjectType:        "project",
		subjectDisplayName: new("Project One"),
		subjectSlug:        new("project-one"),
		beforeSnapshot:     firstBefore,
		afterSnapshot:      firstAfter,
		metadata:           []byte(`{"changed":"name"}`),
	})

	insertedSecond := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:     authCtx.ActiveOrganizationID,
		projectID:          uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:            "user:second",
		actorType:          "user",
		actorDisplayName:   nil,
		actorSlug:          nil,
		action:             "api_key:create",
		subjectID:          "key-1",
		subjectType:        "api_key",
		subjectDisplayName: nil,
		subjectSlug:        nil,
		beforeSnapshot:     nil,
		afterSnapshot:      nil,
		metadata:           secondMetadata,
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 2)
	require.Nil(t, result.NextCursor)

	latest := result.Logs[0]
	require.Equal(t, insertedSecond.String(), latest.ID)
	require.NotNil(t, latest.ProjectID)
	require.Equal(t, authCtx.ProjectID.String(), *latest.ProjectID)
	require.NotNil(t, latest.ProjectSlug)
	require.Equal(t, *authCtx.ProjectSlug, *latest.ProjectSlug)
	require.Equal(t, "user:second", latest.ActorID)
	require.Equal(t, "user", latest.ActorType)
	require.Nil(t, latest.ActorDisplayName)
	require.Nil(t, latest.ActorSlug)
	require.Equal(t, "api_key:create", latest.Action)
	require.Equal(t, "key-1", latest.SubjectID)
	require.Equal(t, "api_key", latest.SubjectType)
	require.Nil(t, latest.SubjectDisplayName)
	require.Nil(t, latest.SubjectSlug)
	require.Nil(t, latest.BeforeSnapshot)
	require.Nil(t, latest.AfterSnapshot)
	require.Equal(t, map[string]any{"ip": "127.0.0.1", "source": "ui"}, latest.Metadata)
	parsedLatestCreatedAt, err := time.Parse(time.RFC3339, latest.CreatedAt)
	require.NoError(t, err)
	require.False(t, parsedLatestCreatedAt.IsZero())

	older := result.Logs[1]
	require.Equal(t, insertedFirst.String(), older.ID)
	require.NotNil(t, older.ProjectID)
	require.Equal(t, authCtx.ProjectID.String(), *older.ProjectID)
	require.NotNil(t, older.ProjectSlug)
	require.Equal(t, *authCtx.ProjectSlug, *older.ProjectSlug)
	require.Equal(t, "First User", *older.ActorDisplayName)
	require.Equal(t, "first-user", *older.ActorSlug)
	require.Equal(t, "Project One", *older.SubjectDisplayName)
	require.Equal(t, "project-one", *older.SubjectSlug)
	require.JSONEq(t, string(firstBefore), string(older.BeforeSnapshot))
	require.JSONEq(t, string(firstAfter), string(older.AfterSnapshot))
	require.Equal(t, map[string]any{"changed": "name"}, older.Metadata)
	parsedOlderCreatedAt, err := time.Parse(time.RFC3339, older.CreatedAt)
	require.NoError(t, err)
	require.False(t, parsedOlderCreatedAt.IsZero())
}

func TestAuditService_List_FiltersByOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	matchingID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "user:current-org",
		actorType:      "user",
		action:         "project:create",
		subjectID:      "subject-current",
		subjectType:    "project",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: "org-other",
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "user:other-org",
		actorType:      "user",
		action:         "project:create",
		subjectID:      "subject-other",
		subjectType:    "project",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, matchingID.String(), result.Logs[0].ID)
}

func TestAuditService_List_OrgScopedLogHasNoProjectFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	insertedID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{},
		actorID:        "user:org-scope",
		actorType:      "user",
		action:         "organization:update",
		subjectID:      "organization-1",
		subjectType:    "organization",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, insertedID.String(), result.Logs[0].ID)
	require.Nil(t, result.Logs[0].ProjectID)
	require.Nil(t, result.Logs[0].ProjectSlug)
}

func TestAuditService_List_FiltersByProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	otherProject := createProject(t, ctx, ti, authCtx.ActiveOrganizationID)

	matchingID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "user:match",
		actorType:      "user",
		action:         "project:update",
		subjectID:      "subject-match",
		subjectType:    "project",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: otherProject.ID, Valid: true},
		actorID:        "user:other-project",
		actorType:      "user",
		action:         "project:update",
		subjectID:      "subject-other-project",
		subjectType:    "project",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{},
		actorID:        "user:org",
		actorType:      "user",
		action:         "project:update",
		subjectID:      "subject-org",
		subjectType:    "project",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  authCtx.ProjectSlug,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, matchingID.String(), result.Logs[0].ID)
	require.NotNil(t, result.Logs[0].ProjectID)
	require.Equal(t, authCtx.ProjectID.String(), *result.Logs[0].ProjectID)
	require.NotNil(t, result.Logs[0].ProjectSlug)
	require.Equal(t, *authCtx.ProjectSlug, *result.Logs[0].ProjectSlug)
}

func TestAuditService_List_FiltersByActorID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	actorID := "user:target"

	matchingID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        actorID,
		actorType:      "user",
		action:         "project:update",
		subjectID:      "subject-target",
		subjectType:    "project",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "user:other",
		actorType:      "user",
		action:         "project:update",
		subjectID:      "subject-other",
		subjectType:    "project",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      &actorID,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, matchingID.String(), result.Logs[0].ID)
	require.Equal(t, actorID, result.Logs[0].ActorID)
}

func TestAuditService_List_FiltersByAction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	action := "api_key:create"

	matchingID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "user:first",
		actorType:      "user",
		action:         action,
		subjectID:      "subject-match",
		subjectType:    "api_key",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "user:second",
		actorType:      "user",
		action:         "project:update",
		subjectID:      "subject-other",
		subjectType:    "project",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       &action,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, matchingID.String(), result.Logs[0].ID)
	require.Equal(t, action, result.Logs[0].Action)
}

func TestAuditService_List_FiltersByActorIDAndAction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	actorID := "user:target"
	action := "project:update"

	matchingID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        actorID,
		actorType:      "user",
		action:         action,
		subjectID:      "subject-match",
		subjectType:    "project",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        actorID,
		actorType:      "user",
		action:         "api_key:create",
		subjectID:      "subject-action-miss",
		subjectType:    "api_key",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "user:other",
		actorType:      "user",
		action:         action,
		subjectID:      "subject-actor-miss",
		subjectType:    "project",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      &actorID,
		Action:       &action,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, matchingID.String(), result.Logs[0].ID)
}

func TestAuditService_List_DeletedProjectStillReturnsProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	deletedProject := createProject(t, ctx, ti, authCtx.ActiveOrganizationID)

	insertedID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: deletedProject.ID, Valid: true},
		actorID:        "user:deleted-project",
		actorType:      "user",
		action:         "project:delete",
		subjectID:      deletedProject.ID.String(),
		subjectType:    "project",
	})

	deleteProject(t, ctx, ti, deletedProject.ID)

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, insertedID.String(), result.Logs[0].ID)
	require.NotNil(t, result.Logs[0].ProjectID)
	require.Equal(t, deletedProject.ID.String(), *result.Logs[0].ProjectID)
	require.NotNil(t, result.Logs[0].ProjectSlug)
	require.Equal(t, deletedProject.Slug, *result.Logs[0].ProjectSlug)
}

func TestAuditService_List_ProjectSlugNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	missingSlug := "missing-project"

	_, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  &missingSlug,
		ActorID:      nil,
		Action:       nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestAuditService_List_InvalidCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	invalidCursor := "not-a-valid-cursor"

	_, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       &invalidCursor,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestAuditService_List_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	insertedIDs := make([]uuid.UUID, 0, 51)
	for i := range 51 {
		insertedIDs = append(insertedIDs, insertAuditLog(t, ctx, ti, auditLogSeed{
			organizationID: authCtx.ActiveOrganizationID,
			projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			actorID:        "user:pagination",
			actorType:      "user",
			action:         "project:update",
			subjectID:      uuid.NewString(),
			subjectType:    "project",
			metadata:       []byte(`{"index":` + jsonNumber(i+1) + `}`),
		}))
	}

	page1, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, page1.Logs, 50)
	require.NotNil(t, page1.NextCursor)

	seen := make(map[string]bool, 51)
	for idx, log := range page1.Logs {
		require.Equal(t, insertedIDs[len(insertedIDs)-1-idx].String(), log.ID)
		require.False(t, seen[log.ID])
		seen[log.ID] = true
	}

	page2, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       page1.NextCursor,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, page2.Logs, 1)
	require.Nil(t, page2.NextCursor)
	require.Equal(t, insertedIDs[0].String(), page2.Logs[0].ID)
	require.False(t, seen[page2.Logs[0].ID])
}

func TestAuditService_List_PaginationWithProjectFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	otherProject := createProject(t, ctx, ti, authCtx.ActiveOrganizationID)

	matchingIDs := make([]uuid.UUID, 0, 51)
	for range 51 {
		matchingIDs = append(matchingIDs, insertAuditLog(t, ctx, ti, auditLogSeed{
			organizationID: authCtx.ActiveOrganizationID,
			projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			actorID:        "user:project-scope",
			actorType:      "user",
			action:         "project:update",
			subjectID:      uuid.NewString(),
			subjectType:    "project",
		}))
	}

	for range 5 {
		insertAuditLog(t, ctx, ti, auditLogSeed{
			organizationID: authCtx.ActiveOrganizationID,
			projectID:      uuid.NullUUID{UUID: otherProject.ID, Valid: true},
			actorID:        "user:other-project",
			actorType:      "user",
			action:         "project:update",
			subjectID:      uuid.NewString(),
			subjectType:    "project",
		})
	}

	page1, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  authCtx.ProjectSlug,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, page1.Logs, 50)
	require.NotNil(t, page1.NextCursor)

	for idx, log := range page1.Logs {
		require.Equal(t, matchingIDs[len(matchingIDs)-1-idx].String(), log.ID)
	}

	page2, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       page1.NextCursor,
		ProjectSlug:  authCtx.ProjectSlug,
		ActorID:      nil,
		Action:       nil,
	})
	require.NoError(t, err)
	require.Len(t, page2.Logs, 1)
	require.Nil(t, page2.NextCursor)
	require.Equal(t, matchingIDs[0].String(), page2.Logs[0].ID)
}

func testAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	return authCtx
}

func insertAuditLog(t *testing.T, ctx context.Context, ti *testInstance, seed auditLogSeed) uuid.UUID {
	t.Helper()

	id, err := repo.New(ti.conn).InsertAuditLog(ctx, repo.InsertAuditLogParams{
		OrganizationID:     seed.organizationID,
		ProjectID:          seed.projectID,
		ActorID:            seed.actorID,
		ActorType:          seed.actorType,
		ActorDisplayName:   conv.PtrToPGTextEmpty(seed.actorDisplayName),
		ActorSlug:          conv.PtrToPGTextEmpty(seed.actorSlug),
		Action:             seed.action,
		SubjectID:          seed.subjectID,
		SubjectType:        seed.subjectType,
		SubjectDisplayName: conv.PtrToPGTextEmpty(seed.subjectDisplayName),
		SubjectSlug:        conv.PtrToPGTextEmpty(seed.subjectSlug),
		BeforeSnapshot:     seed.beforeSnapshot,
		AfterSnapshot:      seed.afterSnapshot,
		Metadata:           seed.metadata,
	})
	require.NoError(t, err)

	return id
}

func createProject(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) projectsrepo.Project {
	t.Helper()

	projectSlug := uuid.NewString()
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return project
}

func deleteProject(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) {
	t.Helper()

	deletedID, err := projectsrepo.New(ti.conn).DeleteProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, projectID, deletedID)
}

func jsonNumber(value int) string {
	return strconv.Itoa(value)
}
