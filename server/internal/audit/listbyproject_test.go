package audit_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestAuditService_ListByProject_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)

	result, err := ti.service.ListByProject(ctx, &gen.ListByProjectPayload{ApikeyToken: nil, SessionToken: nil, Cursor: nil, ProjectSlug: *authCtx.ProjectSlug})
	require.NoError(t, err)
	require.Empty(t, result.Logs)
	require.Nil(t, result.NextCursor)
}

func TestAuditService_ListByProject_PaginatesWithOpaqueCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	for i := range 52 {
		insertAuditLog(t, ctx, ti.conn, insertAuditLogParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			Action:         fmt.Sprintf("test:list:%02d", i),
			SubjectDisplay: fmt.Sprintf("log-%02d", i),
		})
	}

	page1, err := ti.service.ListByProject(ctx, &gen.ListByProjectPayload{ApikeyToken: nil, SessionToken: nil, Cursor: nil, ProjectSlug: *authCtx.ProjectSlug})
	require.NoError(t, err)
	require.Len(t, page1.Logs, 50)
	require.NotNil(t, page1.NextCursor)
	require.NotEmpty(t, page1.Logs[0].ID)
	require.Equal(t, "log-51", conv.PtrValOr(page1.Logs[0].SubjectDisplayName, ""))
	require.Equal(t, "log-02", conv.PtrValOr(page1.Logs[49].SubjectDisplayName, ""))
	require.NotContains(t, *page1.NextCursor, "+")
	require.NotContains(t, *page1.NextCursor, "/")
	require.NotContains(t, *page1.NextCursor, "=")

	page2, err := ti.service.ListByProject(ctx, &gen.ListByProjectPayload{ApikeyToken: nil, SessionToken: nil, Cursor: page1.NextCursor, ProjectSlug: *authCtx.ProjectSlug})
	require.NoError(t, err)
	require.Len(t, page2.Logs, 2)
	require.Nil(t, page2.NextCursor)
	require.Equal(t, "log-01", conv.PtrValOr(page2.Logs[0].SubjectDisplayName, ""))
	require.Equal(t, "log-00", conv.PtrValOr(page2.Logs[1].SubjectDisplayName, ""))
}

func TestAuditService_ListByProject_InvalidCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)

	_, err := ti.service.ListByProject(ctx, &gen.ListByProjectPayload{ApikeyToken: nil, SessionToken: nil, Cursor: conv.PtrEmpty("not-valid!!!"), ProjectSlug: *authCtx.ProjectSlug})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cursor")
}

func TestAuditService_ListByProject_InvalidProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)

	_, err := ti.service.ListByProject(ctx, &gen.ListByProjectPayload{ApikeyToken: nil, SessionToken: nil, Cursor: nil, ProjectSlug: "missing-project"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "resource not found")
}

func TestAuditService_ListByProject_FiltersToRequestedProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "other-project-filter",
		Slug:           "other-project-filter",
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	insertAuditLog(t, ctx, ti.conn, insertAuditLogParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Action:         "test:visible",
		SubjectDisplay: "visible-log",
	})
	insertAuditLog(t, ctx, ti.conn, insertAuditLogParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: otherProject.ID, Valid: true},
		Action:         "test:hidden",
		SubjectDisplay: "hidden-log",
	})

	result, err := ti.service.ListByProject(ctx, &gen.ListByProjectPayload{ApikeyToken: nil, SessionToken: nil, Cursor: nil, ProjectSlug: *authCtx.ProjectSlug})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.NotEmpty(t, result.Logs[0].ID)
	require.Equal(t, "visible-log", conv.PtrValOr(result.Logs[0].SubjectDisplayName, ""))
	require.NotContains(t, fmt.Sprintf("%v", result.Logs), "hidden-log")
}

func TestAuditService_ListByProject_FiltersByProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "other-project",
		Slug:           "other-project",
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	insertAuditLog(t, ctx, ti.conn, insertAuditLogParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Action:         "test:project:current",
		SubjectDisplay: "current-project-log",
	})
	insertAuditLog(t, ctx, ti.conn, insertAuditLogParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: otherProject.ID, Valid: true},
		Action:         "test:project:other",
		SubjectDisplay: "other-project-log",
	})
	insertAuditLog(t, ctx, ti.conn, insertAuditLogParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{},
		Action:         "test:project:none",
		SubjectDisplay: "org-wide-log",
	})

	result, err := ti.service.ListByProject(ctx, &gen.ListByProjectPayload{ApikeyToken: nil, SessionToken: nil, Cursor: nil, ProjectSlug: *authCtx.ProjectSlug})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, "current-project-log", conv.PtrValOr(result.Logs[0].SubjectDisplayName, ""))
}

type insertAuditLogParams struct {
	OrganizationID string
	ProjectID      uuid.NullUUID
	Action         string
	SubjectDisplay string
}

func insertAuditLog(t *testing.T, ctx context.Context, db repo.DBTX, params insertAuditLogParams) {
	t.Helper()

	_, err := repo.New(db).InsertAuditLog(ctx, repo.InsertAuditLogParams{
		OrganizationID:     params.OrganizationID,
		ProjectID:          params.ProjectID,
		ActorID:            "user:test-user",
		ActorType:          "user",
		ActorDisplayName:   conv.ToPGTextEmpty("Test User"),
		ActorSlug:          conv.ToPGTextEmpty("test-user"),
		Action:             params.Action,
		SubjectID:          params.SubjectDisplay,
		SubjectType:        "test_subject",
		SubjectDisplayName: conv.ToPGTextEmpty(params.SubjectDisplay),
		SubjectSlug:        pgtype.Text{},
		BeforeSnapshot:     nil,
		AfterSnapshot:      nil,
		Metadata:           nil,
	})
	require.NoError(t, err)
}
