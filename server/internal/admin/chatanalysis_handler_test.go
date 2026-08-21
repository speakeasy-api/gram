package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type recordingAdminChatAnalysisSignaler struct {
	projectIDs []uuid.UUID
}

func (s *recordingAdminChatAnalysisSignaler) Signal(_ context.Context, projectID uuid.UUID) error {
	s.projectIDs = append(s.projectIDs, projectID)
	return nil
}

func TestChatAnalysisSettingsRequiresAdminSession(t *testing.T) {
	t.Parallel()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-chat-analysis-auth", "operator@example.com")))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)

	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/admin/organization.chatAnalysisSettings"},
		{method: http.MethodPost, path: "/admin/organization.chatAnalysisSettings"},
		{method: http.MethodPost, path: "/admin/organization.chatAnalysisTrigger"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(test.method, test.path, nil))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

func TestChatAnalysisSettingsDefaultsUpdatesAndAudits(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const (
		orgID         = "org_admin_chat_analysis"
		operatorEmail = "operator@example.com"
	)
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Chat Analysis Org", Slug: "admin-chat-analysis", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, operatorEmail)

	get := httptest.NewRequest(http.MethodGet, "/admin/organization.chatAnalysisSettings?organization_id="+orgID, nil)
	get.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	getResult := httptest.NewRecorder()
	handler.ServeHTTP(getResult, get)
	require.Equal(t, http.StatusOK, getResult.Code)
	var defaults chatanalysis.Settings
	require.NoError(t, json.Unmarshal(getResult.Body.Bytes(), &defaults))
	require.Equal(t, chatanalysis.Settings{OrganizationID: orgID, IsDefault: true}, defaults)

	body := []byte(`{"organization_id":"` + orgID + `","judge":"work_units","enabled":true,"daily_cap":321}`)
	post := httptest.NewRequest(http.MethodPost, "/admin/organization.chatAnalysisSettings", bytes.NewReader(body))
	post.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	postResult := httptest.NewRecorder()
	handler.ServeHTTP(postResult, post)
	require.Equal(t, http.StatusOK, postResult.Code)
	var updated chatanalysis.Settings
	require.NoError(t, json.Unmarshal(postResult.Body.Bytes(), &updated))
	require.Equal(t, chatanalysis.Settings{
		OrganizationID: orgID, WorkUnitsEnabled: true, WorkUnitsDailyCap: 321, IsDefault: false,
	}, updated)

	// false and zero are valid required values, not missing fields. This also
	// exercises the second and only other allowlisted judge.
	body = []byte(`{"organization_id":"` + orgID + `","judge":"business_memory","enabled":false,"daily_cap":0}`)
	post = httptest.NewRequest(http.MethodPost, "/admin/organization.chatAnalysisSettings", bytes.NewReader(body))
	post.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	postResult = httptest.NewRecorder()
	handler.ServeHTTP(postResult, post)
	require.Equal(t, http.StatusOK, postResult.Code)
	require.NoError(t, json.Unmarshal(postResult.Body.Bytes(), &updated))
	require.Equal(t, chatanalysis.Settings{
		OrganizationID: orgID, WorkUnitsEnabled: true, WorkUnitsDailyCap: 321, IsDefault: false,
	}, updated)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionChatAnalysisSettingsUpsert)
	require.NoError(t, err)
	require.Equal(t, "sub-admin", entry.ActorID)
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, audit.SpeakeasyTeamActorLabel, *entry.ActorDisplayName)
	for name, field := range map[string]string{
		"actor display name": *entry.ActorDisplayName,
		"actor slug":         entry.ActorSlug,
		"actor id":           entry.ActorID,
		"before snapshot":    string(entry.BeforeSnapshot),
		"after snapshot":     string(entry.AfterSnapshot),
		"metadata":           string(entry.Metadata),
	} {
		require.NotContains(t, field, operatorEmail, "operator email leaked through %s", name)
	}
}

func TestTriggerChatAnalysisSignalsOrganizationProjects(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_admin_chat_analysis_trigger"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Chat Analysis Trigger Org", Slug: "admin-chat-analysis-trigger", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	first, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "trigger-first", Slug: "trigger-first", OrganizationID: orgID,
	})
	require.NoError(t, err)
	second, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "trigger-second", Slug: "trigger-second", OrganizationID: orgID,
	})
	require.NoError(t, err)

	signaler := &recordingAdminChatAnalysisSignaler{}
	svc.chatAnalysisSignaler = signaler
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	body := []byte(`{"organization_id":"` + orgID + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/organization.chatAnalysisTrigger", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var response triggerAdminChatAnalysisResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, triggerAdminChatAnalysisResponse{ProjectsSignaled: 2}, response)
	require.ElementsMatch(t, []uuid.UUID{first.ID, second.ID}, signaler.projectIDs)
}

func TestSetChatAnalysisSettingsRejectsInvalidBodies(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_admin_chat_analysis_reject"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Chat Analysis Reject Org", Slug: "admin-chat-analysis-reject", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")

	for name, body := range map[string]string{
		"missing enabled": `{"organization_id":"` + orgID + `","judge":"work_units","daily_cap":1}`,
		"missing cap":     `{"organization_id":"` + orgID + `","judge":"work_units","enabled":false}`,
		"unknown judge":   `{"organization_id":"` + orgID + `","judge":"other","enabled":true,"daily_cap":1}`,
		"negative cap":    `{"organization_id":"` + orgID + `","judge":"business_memory","enabled":true,"daily_cap":-1}`,
		"oversized cap":   `{"organization_id":"` + orgID + `","judge":"work_units","enabled":true,"daily_cap":10001}`,
		"unknown field":   `{"organization_id":"` + orgID + `","judge":"work_units","enabled":true,"daily_cap":1,"run_now":true}`,
		"trailing JSON":   `{"organization_id":"` + orgID + `","judge":"work_units","enabled":true,"daily_cap":1}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/admin/organization.chatAnalysisSettings", bytes.NewBufferString(body))
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}

	stored, err := chatanalysis.LoadSettings(ctx, conn, orgID)
	require.NoError(t, err)
	require.Equal(t, chatanalysis.Settings{OrganizationID: orgID, IsDefault: true}, stored)
}
