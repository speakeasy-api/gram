package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
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
	require.Equal(t, hooksrollout.CanaryOrganizationSlugs(), result.CanaryOrganizationSlugs)
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

func TestOrganizationHooksRollout_CanaryIgnoresPins(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	canarySlug := hooksrollout.CanaryOrganizationSlugs()[0]
	orgID := createHooksRolloutOrganization(t, ctx, conn, canarySlug)

	result, err := svc.SetOrganizationHooksRollout(ctx, &gen.SetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: orgID, Version: 1})
	require.NoError(t, err)
	require.Equal(t, gen.AdminHooksRolloutSource("canary"), result.Source)
	require.Nil(t, result.EffectiveVersion)
	require.True(t, *result.Eligible)
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
