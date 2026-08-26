package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestListOrganizationActivity_HTTPAuthentication(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	svc.tracer = testenv.NewTracerProvider(t).Tracer("admin_activity_test")
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_http", name: "Test Organization", slug: "activity-http"})
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	path := "/admin/organization.activity?organization_id=org_activity_http"

	for _, configure := range []func(*http.Request){
		func(*http.Request) {},
		func(req *http.Request) {
			req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: "placeholder-tenant-credential"})
		},
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		configure(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}

	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.test")
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/admin/organization.activity", nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListOrganizationActivity_ScopeAndResponse(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_target", name: "Target", slug: "activity-target"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_other", name: "Other", slug: "activity-other"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_empty", name: "Empty", slug: "activity-empty"})

	assertOopsCode(t, callOrganizationActivity(ctx, svc, "missing-org", nil), oops.CodeNotFound)
	assertOopsCode(t, callOrganizationActivity(ctx, svc, "activity-target", nil), oops.CodeNotFound)

	empty, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_empty"})
	require.NoError(t, err)
	require.Empty(t, empty.Logs)
	require.Nil(t, empty.NextCursor)

	targetOrgRow := insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_target", subjectType: "project", action: "target:create"})
	newerUnrelatedOrgRow := insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_other", subjectType: "project", action: "active:create"})
	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: "org_activity_other"}
	otherCtx := contextvalues.SetAuthContext(ctx, authCtx)
	targetOnly, err := svc.ListOrganizationActivity(otherCtx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_target"})
	require.NoError(t, err)
	require.Len(t, targetOnly.Logs, 1)
	require.Equal(t, targetOrgRow.String(), targetOnly.Logs[0].ID)
	require.NotEqual(t, newerUnrelatedOrgRow.String(), targetOnly.Logs[0].ID)
	require.Nil(t, targetOnly.NextCursor)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Test Project", Slug: "test-project", OrganizationID: "org_activity_target",
	})
	require.NoError(t, err)

	insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_other", subjectType: "project", action: "other:create"})
	wanted := insertActivity(t, ctx, conn, activitySeed{
		organizationID: "org_activity_target", projectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		actorID: "user:test-operator", actorType: "user", actorName: pgtype.Text{String: "Test Operator", Valid: true},
		actorSlug: pgtype.Text{String: "test-operator", Valid: true}, action: "project:update",
		subjectID: "project-subject", subjectType: "project", subjectName: pgtype.Text{String: "Test Subject", Valid: true},
		subjectSlug: pgtype.Text{String: "test-subject", Valid: true}, before: []byte(`{"name":"before"}`),
		after: []byte(`{"name":"after"}`), metadata: []byte(`{"source":"test"}`),
		surface: pgtype.Text{String: "admin", Valid: true}, clientID: pgtype.Text{String: "client-placeholder", Valid: true},
	})
	insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_target", subjectType: "assistant", action: "assistant:run"})
	insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_target", subjectType: "trial", subjectName: pgtype.Text{String: "Test Trial", Valid: true}, action: "trial:extend"})
	insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_target", actorID: "system", actorType: "system", subjectType: "organization", action: "system:update"})

	result, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_target"})
	require.NoError(t, err)
	require.Len(t, result.Logs, 5)

	var full, system *gen.AuditLog
	for _, log := range result.Logs {
		if log.ID == wanted.String() {
			full = log
		}
		if log.ActorID == "system" {
			system = log
		}
		require.NotEqual(t, "other:create", log.Action)
	}
	require.NotNil(t, full)
	require.Equal(t, project.ID.String(), *full.ProjectID)
	require.Equal(t, "user:test-operator", full.ActorID)
	require.Equal(t, "user", full.ActorType)
	require.Equal(t, "Test Operator", *full.ActorDisplayName)
	require.Equal(t, "test-operator", *full.ActorSlug)
	require.NotEqual(t, "Speakeasy Team", *full.ActorDisplayName)
	require.Equal(t, "project:update", full.Action)
	require.Equal(t, "test-project", *full.ProjectSlug)
	require.Equal(t, "project-subject", full.SubjectID)
	require.Equal(t, "project", full.SubjectType)
	require.Equal(t, "Test Subject", *full.SubjectDisplayName)
	require.Equal(t, "test-subject", *full.SubjectSlug)
	require.Equal(t, "admin", full.ActingSurface)
	require.Equal(t, "client-placeholder", *full.ActingClientID)
	require.JSONEq(t, `{"name":"before"}`, string(full.BeforeSnapshot))
	require.JSONEq(t, `{"name":"after"}`, string(full.AfterSnapshot))
	require.Equal(t, "test", full.Metadata["source"])
	require.NotEmpty(t, full.CreatedAt)
	require.NotNil(t, system)
	require.Equal(t, "System", *system.ActorDisplayName)
	require.Equal(t, "unknown", system.ActingSurface)

	bad := "not-a-cursor"
	_, err = svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_target", Cursor: &bad})
	assertOopsCode(t, err, oops.CodeBadRequest)

	insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_target", subjectType: "project", action: "invalid-metadata", metadata: []byte("[]")})
	_, err = svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_target"})
	assertOopsCode(t, err, oops.CodeUnexpected)
}

func TestListOrganizationActivity_NewestFirstWithDistinctTimestamps(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_order", name: "Order", slug: "activity-order"})

	oldest := insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_order", subjectType: "project", action: "project:create"})
	middle := insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_order", subjectType: "project", action: "project:update"})
	newest := insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_order", subjectType: "project", action: "project:delete"})
	baseTime := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	queries := auditrepo.New(conn)
	for i, id := range []uuid.UUID{oldest, middle, newest} {
		err := queries.UpdateAuditLogCreatedAtForTesting(ctx, auditrepo.UpdateAuditLogCreatedAtForTestingParams{
			CreatedAt: pgtype.Timestamptz{Time: baseTime.Add(time.Duration(i) * time.Hour), Valid: true},
			Ids:       []uuid.UUID{id},
		})
		require.NoError(t, err)
	}

	result, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_order"})
	require.NoError(t, err)
	require.Len(t, result.Logs, 3)
	require.Equal(t, []string{newest.String(), middle.String(), oldest.String()}, []string{result.Logs[0].ID, result.Logs[1].ID, result.Logs[2].ID})
}

func TestListOrganizationActivity_PaginationEqualTimestampsUsesSeqDescending(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_pages", name: "Pages", slug: "activity-pages"})
	inserted := make([]string, 0, 51)
	for range 51 {
		id := insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_pages", subjectType: "project", action: "project:update"})
		inserted = append(inserted, id.String())
	}
	ids := make([]uuid.UUID, len(inserted))
	for i, id := range inserted {
		ids[i] = uuid.MustParse(id)
	}
	err := auditrepo.New(conn).UpdateAuditLogCreatedAtForTesting(ctx, auditrepo.UpdateAuditLogCreatedAtForTestingParams{
		CreatedAt: pgtype.Timestamptz{Time: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC), Valid: true},
		Ids:       ids,
	})
	require.NoError(t, err)

	first, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_pages"})
	require.NoError(t, err)
	require.Len(t, first.Logs, 50)
	require.NotNil(t, first.NextCursor)
	firstIDs := make([]string, len(first.Logs))
	for i, log := range first.Logs {
		firstIDs[i] = log.ID
	}
	wantFirst := make([]string, 50)
	for i := range wantFirst {
		wantFirst[i] = inserted[50-i]
	}
	require.Equal(t, wantFirst, firstIDs)

	second, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_pages", Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Logs, 1)
	require.Nil(t, second.NextCursor)
	require.Equal(t, inserted[0], second.Logs[0].ID)
}

func TestAdminActivityLog_ResponseMapping(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 25, 12, 34, 56, 0, time.UTC)
	base := auditrepo.ListAuditLogsRow{
		ID: uuid.New(), ActorID: "user:test", ActorType: "user", Action: "project:update",
		SubjectID: uuid.NewString(), SubjectType: "project",
		ActingSurface: pgtype.Text{String: " \t ", Valid: true},
		CreatedAt:     pgtype.Timestamptz{Time: createdAt, Valid: true},
	}

	for _, metadata := range [][]byte{nil, {}, []byte("null")} {
		row := base
		row.Metadata = metadata
		log, err := adminActivityLog(row)
		require.NoError(t, err)
		require.Equal(t, "2026-08-25T12:34:56Z", log.CreatedAt)
		require.Equal(t, "unknown", log.ActingSurface)
		require.Nil(t, log.Metadata)
	}

	for name, metadata := range map[string][]byte{
		"malformed": []byte("{"),
		"array":     []byte("[]"),
		"string":    []byte(`"metadata"`),
		"number":    []byte("1"),
		"boolean":   []byte("true"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			row := base
			row.Metadata = metadata
			_, err := adminActivityLog(row)
			require.ErrorContains(t, err, "unmarshal metadata")
		})
	}
}

func TestListOrganizationActivity(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_trial", name: "Trial", slug: "activity-trial", accountType: "enterprise"})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_activity_trial", endsAt: time.Now().UTC().Add(14 * 24 * time.Hour)})
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{SessionID: "admin-session-placeholder", OIDCSubject: "operator-placeholder", Name: "Test Operator", Email: "operator@example.test"})
	_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_activity_trial", Days: 1})
	require.NoError(t, err)

	result, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_trial"})
	require.NoError(t, err)
	require.NotEmpty(t, result.Logs)
	require.Equal(t, "Test Operator", *result.Logs[0].ActorDisplayName)
	require.NotEqual(t, "Speakeasy Team", *result.Logs[0].ActorDisplayName)
	require.Equal(t, "admin", result.Logs[0].ActingSurface)
}

type activitySeed struct {
	organizationID                 string
	projectID                      uuid.NullUUID
	actorID, actorType             string
	actorName, actorSlug           pgtype.Text
	action, subjectID, subjectType string
	subjectName, subjectSlug       pgtype.Text
	before, after, metadata        []byte
	surface, clientID              pgtype.Text
}

func insertActivity(t *testing.T, ctx context.Context, conn *pgxpool.Pool, seed activitySeed) uuid.UUID {
	t.Helper()
	if seed.actorID == "" {
		seed.actorID = "user:test"
	}
	if seed.actorType == "" {
		seed.actorType = "user"
	}
	if seed.subjectID == "" {
		seed.subjectID = uuid.NewString()
	}
	row, err := auditrepo.New(conn).InsertAuditLog(ctx, auditrepo.InsertAuditLogParams{
		OrganizationID: seed.organizationID, ProjectID: seed.projectID, ActorID: seed.actorID, ActorType: seed.actorType,
		ActorDisplayName: seed.actorName, ActorSlug: seed.actorSlug, Action: seed.action, SubjectID: seed.subjectID,
		SubjectType: seed.subjectType, SubjectDisplayName: seed.subjectName, SubjectSlug: seed.subjectSlug,
		BeforeSnapshot: seed.before, AfterSnapshot: seed.after, Metadata: seed.metadata, ActingSurface: seed.surface, ActingClientID: seed.clientID,
	})
	require.NoError(t, err)
	return row.ID
}

func callOrganizationActivity(ctx context.Context, svc *Service, organizationID string, cursor *string) error {
	_, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: organizationID, Cursor: cursor})
	return err
}

func assertOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}
