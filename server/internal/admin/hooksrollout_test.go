package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const hooksRolloutOperatorEmail = "operator@example.test"

func hooksRolloutContext(ctx context.Context) context.Context {
	return contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "admin-session-placeholder",
		OIDCSubject: "operator-placeholder",
		Name:        "Test Operator",
		Email:       hooksRolloutOperatorEmail,
		HD:          "example.test",
	})
}

func createHooksRolloutOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) string {
	t.Helper()
	organizationID := uuid.NewString()
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 organizationID,
		Name:               "Hooks Rollout Org",
		Slug:               slug,
		GramAccountType:    "free",
		WorkosID:           conv.PtrToPGText(nil),
		FreeTrialStartedAt: conv.ToPGTimestamptz(now),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
		DisabledAt:         conv.PtrToPGTimestamptz(nil),
	}))
	return organizationID
}

func currentHooksVersion(t *testing.T) int {
	t.Helper()
	current, err := plugins.CurrentHooksGeneratorVersion()
	require.NoError(t, err)
	return current
}

func TestHooksRolloutRequiresAdminSession(t *testing.T) {
	t.Parallel()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-hooks-rollout-auth", "operator@example.com")))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/admin/hooksRollout.get", body: ""},
		{method: http.MethodPost, path: "/admin/hooksRollout.setDefault", body: `{"version":1}`},
		{method: http.MethodGet, path: "/admin/organization.hooksRollout?organization_id=org", body: ""},
		{method: http.MethodPost, path: "/admin/organization.setHooksRollout", body: `{"organization_id":"org","version":1}`},
		{method: http.MethodPost, path: "/admin/organization.clearHooksRollout", body: `{"organization_id":"org"}`},
		{method: http.MethodPost, path: "/admin/hooksRollout.startStage", body: `{"revision":0,"version":1}`},
		{method: http.MethodPost, path: "/admin/hooksRollout.pauseStage", body: `{"revision":1,"reason":"alert"}`},
		{method: http.MethodPost, path: "/admin/hooksRollout.resumeStage", body: `{"revision":1}`},
		{method: http.MethodPost, path: "/admin/hooksRollout.cancelStage", body: `{"revision":1,"reason":"alert"}`},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", test.method, test.path)
	}
}

func TestGetHooksRollout_Empty(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)

	result, err := svc.GetHooksRollout(ctx, &gen.GetHooksRolloutPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, currentHooksVersion(t), result.CurrentVersion)
	require.Nil(t, result.DefaultPin)
	require.Empty(t, result.CanaryOrganizationSlugs, "no staff organization is configured")
	require.Empty(t, result.Overrides)
	require.Empty(t, result.RecentChanges)
}

func TestSetHooksRolloutDefault(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	current := currentHooksVersion(t)

	result, err := svc.SetHooksRolloutDefault(ctx, &gen.SetHooksRolloutDefaultPayload{AdminSessionToken: nil, Version: current})
	require.NoError(t, err)
	require.NotNil(t, result.DefaultPin)
	require.Equal(t, current, result.DefaultPin.Version)
	require.Equal(t, hooksRolloutOperatorEmail, result.DefaultPin.SetBy)
	_, err = time.Parse(time.RFC3339, result.DefaultPin.SetAt)
	require.NoError(t, err)
	require.Len(t, result.RecentChanges, 1)
	require.Nil(t, result.RecentChanges[0].OrganizationID)
	require.Equal(t, current, *result.RecentChanges[0].Version)
}

func TestSetHooksRolloutDefault_RejectsVersionAboveCurrent(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)

	_, err := svc.SetHooksRolloutDefault(ctx, &gen.SetHooksRolloutDefaultPayload{AdminSessionToken: nil, Version: currentHooksVersion(t) + 1})
	requireOopsCode(t, err, oops.CodeBadRequest)

	result, err := svc.GetHooksRollout(ctx, &gen.GetHooksRolloutPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Nil(t, result.DefaultPin, "a rejected pin records nothing")
}

func TestSetHooksRolloutDefault_RequiresOperatorEmail(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "admin-session-placeholder", OIDCSubject: "operator-placeholder", Name: "Test Operator", Email: "", HD: "example.test",
	})

	_, err := svc.SetHooksRolloutDefault(ctx, &gen.SetHooksRolloutDefaultPayload{AdminSessionToken: nil, Version: 1})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestOrganizationHooksRollout_LegacyFlagUntilPinned(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	orgID := createHooksRolloutOrganization(t, ctx, conn, "hooks-rollout-legacy")

	result, err := svc.GetOrganizationHooksRollout(ctx, &gen.GetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID})
	require.NoError(t, err)
	require.Equal(t, orgID, result.OrganizationID)
	require.Equal(t, gen.AdminHooksRolloutSource("legacy_flag"), result.Source)
	require.Nil(t, result.EffectiveVersion)
	require.Nil(t, result.Eligible, "the admin server cannot read the legacy flag")
}

func TestOrganizationHooksRollout_OverrideAndDefault(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	current := currentHooksVersion(t)
	orgID := createHooksRolloutOrganization(t, ctx, conn, "hooks-rollout-override")

	_, err := svc.SetHooksRolloutDefault(ctx, &gen.SetHooksRolloutDefaultPayload{AdminSessionToken: nil, Version: current})
	require.NoError(t, err)

	result, err := svc.GetOrganizationHooksRollout(ctx, &gen.GetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID})
	require.NoError(t, err)
	require.Equal(t, gen.AdminHooksRolloutSource("default"), result.Source)
	require.Equal(t, current, *result.EffectiveVersion)
	require.True(t, *result.Eligible)

	result, err = svc.SetOrganizationHooksRollout(ctx, &gen.SetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID, Version: current - 1})
	require.NoError(t, err)
	require.Equal(t, gen.AdminHooksRolloutSource("organization"), result.Source)
	require.Equal(t, current-1, *result.EffectiveVersion)
	require.False(t, *result.Eligible, "an override below current holds the organization back")
	require.NotNil(t, result.Override)
	require.Equal(t, hooksRolloutOperatorEmail, result.Override.SetBy)
	require.Equal(t, current, result.DefaultPin.Version)

	platform, err := svc.GetHooksRollout(ctx, &gen.GetHooksRolloutPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Len(t, platform.Overrides, 1)
	require.Equal(t, orgID, platform.Overrides[0].OrganizationID)
	require.Equal(t, "hooks-rollout-override", platform.Overrides[0].OrganizationSlug)
	require.Equal(t, current-1, platform.Overrides[0].Pin.Version)
	require.Equal(t, "hooks-rollout-override", *platform.RecentChanges[0].OrganizationSlug)

	result, err = svc.ClearOrganizationHooksRollout(ctx, &gen.ClearOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID})
	require.NoError(t, err)
	require.Equal(t, gen.AdminHooksRolloutSource("default"), result.Source)
	require.Nil(t, result.Override)
	require.True(t, *result.Eligible)

	platform, err = svc.GetHooksRollout(ctx, &gen.GetHooksRolloutPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, platform.Overrides)
	require.Len(t, platform.RecentChanges, 3)
	require.Nil(t, platform.RecentChanges[0].Version, "the newest change cleared the override")
}

func TestGetHooksRollout_ListsStaffOrganizationAsCanary(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	svc.staffOrg = staff.NewOrganization(createHooksRolloutOrganization(t, ctx, conn, "hooks-rollout-staff"))

	result, err := svc.GetHooksRollout(ctx, &gen.GetHooksRolloutPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, []string{"hooks-rollout-staff"}, result.CanaryOrganizationSlugs)
}

func TestOrganizationHooksRollout_StaffOrganizationIgnoresPins(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	orgID := createHooksRolloutOrganization(t, ctx, conn, "hooks-rollout-staff-pins")
	svc.staffOrg = staff.NewOrganization(orgID)

	result, err := svc.SetOrganizationHooksRollout(ctx, &gen.SetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID, Version: 1})
	require.NoError(t, err)
	require.Equal(t, gen.AdminHooksRolloutSource("canary"), result.Source)
	require.Nil(t, result.EffectiveVersion)
	require.True(t, *result.Eligible)
}

func TestOrganizationHooksRollout_StaffSlugIsNotACanary(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	orgID := createHooksRolloutOrganization(t, ctx, conn, "speakeasy-team")

	result, err := svc.SetOrganizationHooksRollout(ctx, &gen.SetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID, Version: 1})
	require.NoError(t, err)
	require.Equal(t, gen.AdminHooksRolloutSource("organization"), result.Source, "the canary follows the staff organization ID, not a slug")
}

func TestOrganizationHooksRollout_UnknownOrganization(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	missing := uuid.NewString()

	_, err := svc.GetOrganizationHooksRollout(ctx, &gen.GetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: missing})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = svc.SetOrganizationHooksRollout(ctx, &gen.SetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: missing, Version: 1})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = svc.ClearOrganizationHooksRollout(ctx, &gen.ClearOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: missing})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetOrganizationHooksRollout_RejectsVersionAboveCurrent(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	orgID := createHooksRolloutOrganization(t, ctx, conn, "hooks-rollout-too-high")

	_, err := svc.SetOrganizationHooksRollout(ctx, &gen.SetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID, Version: currentHooksVersion(t) + 1})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestHooksRolloutStage_Lifecycle(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	current := currentHooksVersion(t)

	_, err := svc.SetHooksRolloutDefault(ctx, &gen.SetHooksRolloutDefaultPayload{AdminSessionToken: nil, Version: current - 1})
	require.NoError(t, err)

	result, err := svc.StartHooksRolloutStage(ctx, &gen.StartHooksRolloutStagePayload{AdminSessionToken: nil, Revision: 0, Version: current})
	require.NoError(t, err)
	require.NotNil(t, result.Stage)
	require.Equal(t, current, result.Stage.Version)
	require.Equal(t, 0, result.Stage.Percent)
	require.Equal(t, "running", result.Stage.State)
	require.Equal(t, hooksRolloutOperatorEmail, result.Stage.SetBy)
	require.NotNil(t, result.Stage.NextAdvanceAt)
	require.Positive(t, result.Stage.Revision)

	_, err = svc.ResumeHooksRolloutStage(ctx, &gen.ResumeHooksRolloutStagePayload{AdminSessionToken: nil, Revision: result.Stage.Revision})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = svc.PauseHooksRolloutStage(ctx, &gen.PauseHooksRolloutStagePayload{AdminSessionToken: nil, Revision: result.Stage.Revision, Reason: "   "})
	requireOopsCode(t, err, oops.CodeBadRequest)

	result, err = svc.PauseHooksRolloutStage(ctx, &gen.PauseHooksRolloutStagePayload{AdminSessionToken: nil, Revision: result.Stage.Revision, Reason: " hooks alert "})
	require.NoError(t, err)
	require.Equal(t, "paused", result.Stage.State)
	require.Equal(t, "hooks alert", *result.Stage.Reason)
	require.Nil(t, result.Stage.NextAdvanceAt)

	result, err = svc.ResumeHooksRolloutStage(ctx, &gen.ResumeHooksRolloutStagePayload{AdminSessionToken: nil, Revision: result.Stage.Revision})
	require.NoError(t, err)
	require.Equal(t, "running", result.Stage.State)
	require.Nil(t, result.Stage.Reason)

	result, err = svc.CancelHooksRolloutStage(ctx, &gen.CancelHooksRolloutStagePayload{AdminSessionToken: nil, Revision: result.Stage.Revision, Reason: "bad release"})
	require.NoError(t, err)
	require.Equal(t, "cancelled", result.Stage.State)
	require.Equal(t, current-1, result.DefaultPin.Version, "cancelling leaves the default pin alone")

	_, err = svc.CancelHooksRolloutStage(ctx, &gen.CancelHooksRolloutStagePayload{AdminSessionToken: nil, Revision: result.Stage.Revision, Reason: "again"})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestHooksRolloutStage_StaleRevisionConflicts(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	current := currentHooksVersion(t)

	_, err := svc.SetHooksRolloutDefault(ctx, &gen.SetHooksRolloutDefaultPayload{AdminSessionToken: nil, Version: current - 2})
	require.NoError(t, err)
	shown, err := svc.StartHooksRolloutStage(ctx, &gen.StartHooksRolloutStagePayload{AdminSessionToken: nil, Revision: 0, Version: current - 1})
	require.NoError(t, err)

	_, err = svc.StartHooksRolloutStage(ctx, &gen.StartHooksRolloutStagePayload{AdminSessionToken: nil, Revision: 0, Version: current})
	requireOopsCode(t, err, oops.CodeConflict)
	replaced, err := svc.StartHooksRolloutStage(ctx, &gen.StartHooksRolloutStagePayload{AdminSessionToken: nil, Revision: shown.Stage.Revision, Version: current})
	require.NoError(t, err)

	_, err = svc.CancelHooksRolloutStage(ctx, &gen.CancelHooksRolloutStagePayload{AdminSessionToken: nil, Revision: shown.Stage.Revision, Reason: "cancel the old one"})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = svc.PauseHooksRolloutStage(ctx, &gen.PauseHooksRolloutStagePayload{AdminSessionToken: nil, Revision: shown.Stage.Revision, Reason: "pause the old one"})
	requireOopsCode(t, err, oops.CodeConflict)

	result, err := svc.GetHooksRollout(ctx, &gen.GetHooksRolloutPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, replaced.Stage.Revision, result.Stage.Revision)
	require.Equal(t, "running", result.Stage.State)
}

func TestResumeHooksRolloutStage_RejectsBodylessPost(t *testing.T) {
	t.Parallel()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("resume-subject", "operator@example.com")))
	sessionID, err := svc.sessions.Store(t.Context(), StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "resume-subject", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)

	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		req := httptest.NewRequest(http.MethodPost, "/admin/hooksRollout.resumeStage", strings.NewReader(""))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		SessionMiddleware(mux).ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code, "content type %q: %s", contentType, rec.Body.String())
	}
}

func TestStartHooksRolloutStage_RejectsInvalidVersions(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	current := currentHooksVersion(t)

	_, err := svc.StartHooksRolloutStage(ctx, &gen.StartHooksRolloutStagePayload{AdminSessionToken: nil, Revision: 0, Version: current + 1})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = svc.SetHooksRolloutDefault(ctx, &gen.SetHooksRolloutDefaultPayload{AdminSessionToken: nil, Version: current})
	require.NoError(t, err)
	_, err = svc.StartHooksRolloutStage(ctx, &gen.StartHooksRolloutStagePayload{AdminSessionToken: nil, Revision: 0, Version: current})
	requireOopsCode(t, err, oops.CodeBadRequest)

	result, err := svc.GetHooksRollout(ctx, &gen.GetHooksRolloutPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Nil(t, result.Stage, "a rejected start records nothing")
}

func TestOrganizationHooksRollout_ClearedByStage(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	current := currentHooksVersion(t)

	var orgID string
	for i := 0; orgID == ""; i++ {
		require.Less(t, i, 200, "no organization fell in the first 25 percent")
		id := createHooksRolloutOrganization(t, ctx, conn, "hooks-rollout-stage-"+strconv.Itoa(i))
		if hooksrollout.InStage(id, 25) {
			orgID = id
		}
	}

	_, err := svc.SetHooksRolloutDefault(ctx, &gen.SetHooksRolloutDefaultPayload{AdminSessionToken: nil, Version: current - 1})
	require.NoError(t, err)
	_, err = svc.StartHooksRolloutStage(ctx, &gen.StartHooksRolloutStagePayload{AdminSessionToken: nil, Revision: 0, Version: current})
	require.NoError(t, err)

	result, err := svc.GetOrganizationHooksRollout(ctx, &gen.GetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID})
	require.NoError(t, err)
	require.Equal(t, gen.AdminHooksRolloutSource("default"), result.Source, "the first stage clears canaries only")

	_, err = hooksrollout.NewStageStore(conn).Advance(ctx, time.Now().Add(hooksrollout.StageSoak+time.Minute))
	require.NoError(t, err)

	result, err = svc.GetOrganizationHooksRollout(ctx, &gen.GetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID})
	require.NoError(t, err)
	require.Equal(t, gen.AdminHooksRolloutSource("stage"), result.Source)
	require.Equal(t, current, *result.EffectiveVersion)
	require.True(t, *result.Eligible)
}
