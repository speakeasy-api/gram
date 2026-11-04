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
	"github.com/speakeasy-api/gram/server/internal/audit"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	securityrepo "github.com/speakeasy-api/gram/server/internal/audit/securityevent/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

func TestListOrganizationActivity_OmitsNonObjectMetadata(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_non_object_metadata", name: "Metadata", slug: "activity-metadata"})

	for _, metadata := range [][]byte{[]byte("[]"), []byte(`"metadata"`), []byte("1"), []byte("true")} {
		insertActivity(t, ctx, conn, activitySeed{
			organizationID: "org_activity_non_object_metadata",
			subjectType:    "project",
			action:         "project:update",
			metadata:       metadata,
		})
	}

	result, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: "org_activity_non_object_metadata"})
	require.NoError(t, err)
	require.Len(t, result.Logs, 4)
	for _, log := range result.Logs {
		require.Nil(t, log.Metadata)
	}
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

	for _, metadata := range [][]byte{nil, {}, []byte("null"), []byte("[]"), []byte(`"metadata"`), []byte("1"), []byte("true")} {
		row := base
		row.Metadata = metadata
		log, err := adminActivityLog(row, audit.CategoryConfiguration)
		require.NoError(t, err)
		require.Equal(t, "2026-08-25T12:34:56Z", log.CreatedAt)
		require.Equal(t, "unknown", log.ActingSurface)
		require.Nil(t, log.Metadata)
	}

	for name, metadata := range map[string][]byte{
		"malformed": []byte("{"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			row := base
			row.Metadata = metadata
			_, err := adminActivityLog(row, audit.CategoryConfiguration)
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
	security                       bool
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
	params := auditrepo.InsertAuditLogParams{
		OrganizationID: seed.organizationID, ProjectID: seed.projectID, ActorID: seed.actorID, ActorType: seed.actorType,
		ActorDisplayName: seed.actorName, ActorSlug: seed.actorSlug, Action: seed.action, SubjectID: seed.subjectID,
		SubjectType: seed.subjectType, SubjectDisplayName: seed.subjectName, SubjectSlug: seed.subjectSlug,
		BeforeSnapshot: seed.before, AfterSnapshot: seed.after, Metadata: seed.metadata, ActingSurface: seed.surface, ActingClientID: seed.clientID,
	}
	if seed.security {
		row, err := securityrepo.New(conn).InsertSecurityAuditLog(ctx, securityrepo.InsertSecurityAuditLogParams(params))
		require.NoError(t, err)
		return row.ID
	}
	row, err := auditrepo.New(conn).InsertAuditLog(ctx, params)
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

func TestListOrganizationActivityFiltered_Predicates(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_activity_filters"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Filters", slug: "activity-filters"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_foreign", name: "Foreign", slug: "activity-foreign"})
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Project", Slug: "project", OrganizationID: orgID})
	require.NoError(t, err)
	base := activitySeed{
		organizationID: orgID, projectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		actorID: "actor-match", action: "resource:update", subjectType: "assistant", subjectID: "subject-match",
		surface: pgtype.Text{String: "future_surface", Valid: true}, before: []byte(`{"before":true}`),
		after: []byte(`{"after":true}`), metadata: []byte(`{"context":"unchanged"}`),
	}
	wanted := insertActivity(t, ctx, conn, base)
	var otherIDs []uuid.UUID
	for _, change := range []func(*activitySeed){
		func(s *activitySeed) { s.projectID = uuid.NullUUID{} },
		func(s *activitySeed) { s.actorID = "other-actor" },
		func(s *activitySeed) { s.action = "resource:create" },
		func(s *activitySeed) { s.subjectType = "project" },
		func(s *activitySeed) { s.subjectID = "other-subject" },
		func(s *activitySeed) { s.surface = pgtype.Text{} },
	} {
		seed := base
		change(&seed)
		otherIDs = append(otherIDs, insertActivity(t, ctx, conn, seed))
	}
	foreign := base
	foreign.organizationID = "org_activity_foreign"
	insertActivity(t, ctx, conn, foreign)
	for _, tc := range []struct {
		name   string
		filter OrganizationActivityFilter
		omit   int
	}{
		{"project", OrganizationActivityFilter{ProjectID: base.projectID}, 0},
		{"actor", OrganizationActivityFilter{ActorID: &base.actorID}, 1},
		{"action", OrganizationActivityFilter{Action: &base.action}, 2},
		{"subject type", OrganizationActivityFilter{SubjectType: &base.subjectType}, 3},
		{"subject id", OrganizationActivityFilter{SubjectID: &base.subjectID}, 4},
		{"surface", OrganizationActivityFilter{ActingSurface: new("future_surface")}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.filter.OrganizationID = orgID
			result, err := svc.ListOrganizationActivityFiltered(ctx, tc.filter)
			require.NoError(t, err)
			require.Len(t, result.Logs, 6)
			for _, row := range result.Logs {
				require.NotEqual(t, otherIDs[tc.omit].String(), row.ID)
			}
		})
	}
	combined, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{
		OrganizationID: orgID, ProjectID: base.projectID, ActorID: &base.actorID, Action: &base.action,
		SubjectType: &base.subjectType, SubjectID: &base.subjectID, ActingSurface: new("future_surface"),
	})
	require.NoError(t, err)
	require.Len(t, combined.Logs, 1)
	require.Equal(t, wanted.String(), combined.Logs[0].ID)
	require.JSONEq(t, string(base.before), string(combined.Logs[0].BeforeSnapshot))
	require.JSONEq(t, string(base.after), string(combined.Logs[0].AfterSnapshot))
	require.Equal(t, "unchanged", combined.Logs[0].Metadata["context"])
	unknown, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{OrganizationID: orgID, ActingSurface: new("unknown")})
	require.NoError(t, err)
	require.Len(t, unknown.Logs, 1)
	require.Equal(t, otherIDs[5].String(), unknown.Logs[0].ID)
	missing, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{OrganizationID: orgID, ActingSurface: new("not_recorded")})
	require.NoError(t, err)
	require.Empty(t, missing.Logs)
}

// Staff see both logs in one timeline, including security entries the
// customer audit log page leaves out.
func TestListOrganizationActivityFiltered_IncludesEveryCategory(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_categories", name: "Categories", slug: "activity-categories"})
	security := insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_categories", actorID: "kit-runtime", actorType: "system", subjectType: "workload_admission", action: "workload-admission:admit", security: true})
	configuration := insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_categories", subjectType: "project", action: "project:update"})

	result, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{OrganizationID: "org_activity_categories"})
	require.NoError(t, err)
	categories := make(map[string]string, len(result.Logs))
	for _, log := range result.Logs {
		categories[log.ID] = conv.PtrValOrEmpty(log.Category, "")
	}
	require.Equal(t, map[string]string{security.String(): "security", configuration.String(): "configuration"}, categories)
}

// Each log pages by its own seq, so paging the merged timeline must return
// every entry of both exactly once, newest first, whichever log runs out first.
func TestListOrganizationActivity_PagesAcrossBothLogs(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_activity_merged_pages"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Merged", slug: "activity-merged-pages"})

	// 70 configuration entries an hour apart, and 40 security entries placed
	// between the newest 40 of them, so the security log is exhausted while
	// configuration entries remain.
	base := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	want := map[string]time.Time{}
	for i := range 70 {
		id := insertActivity(t, ctx, conn, activitySeed{organizationID: orgID, subjectType: "project", action: "project:update"})
		at := base.Add(time.Duration(i) * time.Hour)
		require.NoError(t, auditrepo.New(conn).UpdateAuditLogCreatedAtForTesting(ctx, auditrepo.UpdateAuditLogCreatedAtForTestingParams{
			CreatedAt: pgtype.Timestamptz{Time: at, Valid: true}, Ids: []uuid.UUID{id},
		}))
		want[id.String()] = at
	}
	for i := range 40 {
		id := insertActivity(t, ctx, conn, activitySeed{organizationID: orgID, subjectType: "workload_admission", action: "workload-admission:admit", security: true})
		at := base.Add(time.Duration(30+i)*time.Hour + 30*time.Minute)
		require.NoError(t, audittestrepo.New(conn).BackdateSecurityAuditLog(ctx, audittestrepo.BackdateSecurityAuditLogParams{
			CreatedAt: pgtype.Timestamptz{Time: at, Valid: true}, ID: id,
		}))
		want[id.String()] = at
	}

	var seen []string
	var cursor *string
	for page := 0; ; page++ {
		require.Less(t, page, 5, "pagination must terminate")
		result, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: orgID, Cursor: cursor})
		require.NoError(t, err)
		require.LessOrEqual(t, len(result.Logs), 50)
		for _, log := range result.Logs {
			seen = append(seen, log.ID)
		}
		if result.NextCursor == nil {
			break
		}
		cursor = result.NextCursor
	}

	require.Len(t, seen, len(want))
	unique := map[string]bool{}
	for i, id := range seen {
		require.False(t, unique[id], "entry %s listed twice", id)
		unique[id] = true
		if i > 0 {
			require.False(t, want[id].After(want[seen[i-1]]), "entries are newest first")
		}
	}
}

func TestListOrganizationActivityFiltered_ProjectIsolation(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_project", name: "Target", slug: "activity-project"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_foreign_project", name: "Foreign", slug: "activity-foreign-project"})
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Foreign", Slug: "foreign", OrganizationID: "org_activity_foreign_project"})
	require.NoError(t, err)
	// Audit storage has no project foreign key; even a matching row cannot authorize the target.
	insertActivity(t, ctx, conn, activitySeed{organizationID: "org_activity_project", projectID: uuid.NullUUID{UUID: project.ID, Valid: true}, subjectType: "project", action: "project:update"})
	var targetErrors []error
	for _, projectID := range []uuid.UUID{project.ID, uuid.New()} {
		result, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{
			OrganizationID: "org_activity_project", ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		})
		require.Nil(t, result)
		assertOopsCode(t, err, oops.CodeNotFound)
		targetErrors = append(targetErrors, err)
	}
	require.Equal(t, targetErrors[0].Error(), targetErrors[1].Error())
	for _, orgID := range []string{"activity-project", "missing-org"} {
		_, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{OrganizationID: orgID})
		assertOopsCode(t, err, oops.CodeNotFound)
	}
}

func TestListOrganizationActivityFiltered_DeletedProject(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_activity_deleted_project"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Deleted project", slug: "activity-deleted-project"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_activity_other_deleted_project", name: "Other", slug: "activity-other-deleted-project"})
	projects := projectsrepo.New(conn)
	project, err := projects.CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Deleted", Slug: "deleted", OrganizationID: orgID})
	require.NoError(t, err)
	id := insertActivity(t, ctx, conn, activitySeed{
		organizationID: orgID, projectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		subjectType: "project", subjectID: project.ID.String(), action: "project:delete",
	})
	_, err = projects.DeleteProject(ctx, project.ID)
	require.NoError(t, err)
	filter := OrganizationActivityFilter{OrganizationID: orgID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}}
	result, err := svc.ListOrganizationActivityFiltered(ctx, filter)
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, id.String(), result.Logs[0].ID)
	require.Equal(t, "project:delete", result.Logs[0].Action)
	filter.OrganizationID = "org_activity_other_deleted_project"
	result, err = svc.ListOrganizationActivityFiltered(ctx, filter)
	require.Nil(t, result)
	assertOopsCode(t, err, oops.CodeNotFound)
}

func TestListOrganizationActivityFiltered_TimeBounds(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_activity_time_filters"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Time", slug: "activity-time-filters"})
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	var ids []uuid.UUID
	for _, at := range []time.Time{from.Add(-time.Second), from, to.Add(-time.Second), to} {
		id := insertActivity(t, ctx, conn, activitySeed{organizationID: orgID, subjectType: "assistant", action: "assistant:run"})
		require.NoError(t, auditrepo.New(conn).UpdateAuditLogCreatedAtForTesting(ctx, auditrepo.UpdateAuditLogCreatedAtForTestingParams{
			CreatedAt: pgtype.Timestamptz{Time: at, Valid: true}, Ids: []uuid.UUID{id},
		}))
		ids = append(ids, id)
	}
	for _, tc := range []struct {
		name     string
		from, to *time.Time
		want     []uuid.UUID
	}{
		{"both", &from, &to, []uuid.UUID{ids[2], ids[1]}},
		{"from only", &from, nil, []uuid.UUID{ids[3], ids[2], ids[1]}},
		{"to only", nil, &to, []uuid.UUID{ids[2], ids[1], ids[0]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{OrganizationID: orgID, From: tc.from, To: tc.to})
			require.NoError(t, err)
			var got []uuid.UUID
			for _, row := range result.Logs {
				got = append(got, uuid.MustParse(row.ID))
			}
			require.Equal(t, tc.want, got)
		})
	}
	for _, end := range []time.Time{from, from.Add(-time.Hour)} {
		_, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{OrganizationID: orgID, From: &from, To: &end})
		assertOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestListOrganizationActivityFiltered_PaginationAndDelegation(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_activity_filtered_pages"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Pages", slug: "activity-filtered-pages"})
	var wanted, allIDs []uuid.UUID
	for range 53 {
		id := insertActivity(t, ctx, conn, activitySeed{organizationID: orgID, actorID: "wanted-actor", subjectType: "assistant", action: "assistant:run"})
		wanted = append(wanted, id)
		allIDs = append(allIDs, id, insertActivity(t, ctx, conn, activitySeed{organizationID: orgID, actorID: "other-actor", subjectType: "assistant", action: "assistant:run"}))
	}
	require.NoError(t, auditrepo.New(conn).UpdateAuditLogCreatedAtForTesting(ctx, auditrepo.UpdateAuditLogCreatedAtForTestingParams{
		CreatedAt: pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}, Ids: allIDs,
	}))
	filter := OrganizationActivityFilter{OrganizationID: orgID, ActorID: new("wanted-actor")}
	first, err := svc.ListOrganizationActivityFiltered(ctx, filter)
	require.NoError(t, err)
	require.Len(t, first.Logs, 50)
	require.NotNil(t, first.NextCursor)
	filter.Cursor = first.NextCursor
	second, err := svc.ListOrganizationActivityFiltered(ctx, filter)
	require.NoError(t, err)
	require.Len(t, second.Logs, 3)
	require.Nil(t, second.NextCursor)
	for i, row := range append(first.Logs, second.Logs...) {
		require.Equal(t, wanted[len(wanted)-1-i].String(), row.ID)
		require.Equal(t, "assistant", row.SubjectType)
	}
	var cursor *string
	for {
		original, err := svc.ListOrganizationActivity(ctx, &gen.ListOrganizationActivityPayload{OrganizationID: orgID, Cursor: cursor})
		require.NoError(t, err)
		filtered, err := svc.ListOrganizationActivityFiltered(ctx, OrganizationActivityFilter{OrganizationID: orgID, Cursor: cursor})
		require.NoError(t, err)
		require.Equal(t, original, filtered)
		cursor = original.NextCursor
		if cursor == nil {
			break
		}
	}
}
