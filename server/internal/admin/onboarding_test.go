package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestOnboardingHTTP(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_onboarding_test"
	const otherID = "org_onboarding_other"
	for _, id := range []string{orgID, otherID} {
		require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
			ID: id, Name: "Onboarding Test", Slug: id, GramAccountType: "enterprise",
			FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour)),
		}))
	}
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	session := makeAdminFeatureSession(t, ctx, svc, "operator@example.test")
	request := func(method, body, cookie string) *httptest.ResponseRecorder {
		t.Helper()
		path := "/admin/organization.onboarding"
		if method == http.MethodGet {
			path += "?organization_id=" + orgID
		}
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		require.Equal(t, http.StatusUnauthorized, request(method, "{", "").Code)
	}
	legacy, err := organizations.LoadOnboardingConfiguration(ctx, conn, orgID)
	require.NoError(t, err)
	require.Nil(t, legacy.Preset)
	require.Len(t, legacy.Tasks, 13)
	require.Len(t, legacy.Presets, 2)
	require.Len(t, legacy.Presets[1].VisibleTaskKeys, 10)
	require.Equal(t, http.StatusOK, request(http.MethodGet, "", session).Code)
	for _, body := range []string{
		`{"organization_id":"org_onboarding_test","visible_task_keys":[],"preset":null}`,
		`{"organization_id":"org_onboarding_test","visible_task_keys":[],"preset":"other"}`,
		`{"organization_id":"org_onboarding_test","visible_task_keys":["unknown"]}`,
		`{"organization_id":"org_onboarding_test"}`,
		`{"organization_id":"org_onboarding_test","visible_task_keys":null}`,
		`{"organization_id":"org_onboarding_test","visible_task_keys":[],"extra":true}`,
		`{"organization_id":"org_onboarding_test","visible_task_keys":[]}{}`,
	} {
		rec := request(http.MethodPost, body, session)
		require.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", body, rec.Body.String())
	}
	unchanged, err := organizations.LoadOnboardingConfiguration(ctx, conn, orgID)
	require.NoError(t, err)
	require.Equal(t, legacy, unchanged)
	// Custom selection on an untouched organization must not invent a preset.
	rec := request(http.MethodPost, `{"organization_id":"org_onboarding_test","visible_task_keys":[]}`, session)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	empty, err := organizations.LoadOnboardingConfiguration(ctx, conn, orgID)
	require.NoError(t, err)
	require.Nil(t, empty.Preset)
	for _, task := range empty.Tasks {
		require.True(t, task.Hidden)
	}
	for _, preset := range legacy.Presets {
		body, err := json.Marshal(map[string]any{"organization_id": orgID, "preset": preset.Key, "visible_task_keys": preset.VisibleTaskKeys})
		require.NoError(t, err)
		rec = request(http.MethodPost, string(body), session)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		config, err := organizations.LoadOnboardingConfiguration(ctx, conn, orgID)
		require.NoError(t, err)
		require.Equal(t, preset.Key, *config.Preset)
		var visible []string
		for _, task := range config.Tasks {
			if !task.Hidden {
				visible = append(visible, task.Key)
			}
		}
		require.ElementsMatch(t, preset.VisibleTaskKeys, visible)
	}
	rec = request(http.MethodPost, `{"organization_id":"org_onboarding_test","visible_task_keys":["platform-mcp"]}`, session)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	custom, err := organizations.LoadOnboardingConfiguration(ctx, conn, orgID)
	require.NoError(t, err)
	require.Equal(t, "security", *custom.Preset)
	other, err := organizations.LoadOnboardingConfiguration(ctx, conn, otherID)
	require.NoError(t, err)
	other.OrganizationID = orgID
	require.Equal(t, legacy, other)
	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationOnboardingUpdated)
	require.NoError(t, err)
	require.Equal(t, "sub-admin", entry.ActorID)
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
	require.Equal(t, orgID, entry.SubjectID)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "security", after["preset"])
	require.Equal(t, []any{"platform-mcp"}, after["visible_task_keys"])
	rows, err := orgrepo.New(conn).ListOrganizationSetupTasks(ctx, orgID)
	require.NoError(t, err)
	for _, row := range rows {
		require.Equal(t, "todo", row.Status)
		require.False(t, row.AssigneeUserID.Valid)
		require.False(t, row.AssigneeEmail.Valid)
	}
}
