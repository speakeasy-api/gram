package dataexports_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// setRouteEnabled runs the core in its own committed transaction, as the
// Platform MCP receipt executor does.
func setRouteEnabled(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID, routeID uuid.UUID, enabled bool) (dataexports.SetRouteEnabledResult, error) {
	t.Helper()
	core := dataexports.NewRouteEnabledCore(audit.NewLogger(), ti.enc)
	require.NotNil(t, core)
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction, as the Platform MCP receipt executor supplies one
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	result, err := core.SetEnabled(ctx, tx, dataexports.SetRouteEnabledParams{
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		RouteID:          routeID,
		Enabled:          enabled,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "user-route-enabled-test"),
		ActorDisplayName: nil,
	})
	if err != nil {
		return result, fmt.Errorf("set route enabled: %w", err)
	}
	require.NoError(t, tx.Commit(ctx))
	return result, nil
}

func routeRow(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID, routeID uuid.UUID) repo.DataExportRoute {
	t.Helper()
	rows, err := repo.New(ti.conn).ListDataExportRoutes(ctx, repo.ListDataExportRoutesParams{OrganizationID: organizationID, ProjectID: projectID})
	require.NoError(t, err)
	for _, row := range rows {
		if row.ID == routeID {
			return row
		}
	}
	require.FailNow(t, "route not found")
	return repo.DataExportRoute{}
}

// withoutToggledFields blanks the two columns pause and resume are allowed to
// change, so whatever remains must be byte-identical across the change.
func withoutToggledFields(row repo.DataExportRoute) repo.DataExportRoute {
	row.Enabled = false
	row.UpdatedAt = row.CreatedAt
	return row
}

func TestRouteEnabledCorePauseAndResumeChangeOnlyEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	created, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "risk_findings", Enabled: true, OtelDestinationID: &destination.ID})
	require.NoError(t, err)
	routeID := uuid.MustParse(created.ID)
	original := routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID)
	require.True(t, original.Enabled)

	paused, err := setRouteEnabled(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID, false)
	require.NoError(t, err)
	require.True(t, paused.Changed)
	afterPause := routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID)
	require.False(t, afterPause.Enabled)
	require.Equal(t, withoutToggledFields(original), withoutToggledFields(afterPause), "pausing must change nothing but enabled and updated_at")
	require.Equal(t, uuid.NullUUID{UUID: uuid.MustParse(destination.ID), Valid: true}, afterPause.OtelDestinationID)
	require.Equal(t, "risk_findings", afterPause.DataSource)

	resumed, err := setRouteEnabled(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID, true)
	require.NoError(t, err)
	require.True(t, resumed.Changed)
	afterResume := routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID)
	require.True(t, afterResume.Enabled)
	require.Equal(t, withoutToggledFields(original), withoutToggledFields(afterResume), "resuming must change nothing but enabled and updated_at")

	for _, expected := range []struct {
		action       audit.Action
		enabledAfter bool
	}{
		{action: audit.ActionDataExportRoutePause, enabledAfter: false},
		{action: audit.ActionDataExportRouteResume, enabledAfter: true},
	} {
		action, enabledAfter := expected.action, expected.enabledAfter
		count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
		require.NoError(t, err)
		require.EqualValues(t, 1, count, "%s", action)
		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, action)
		require.NoError(t, err)
		require.Equal(t, created.ID, record.SubjectID)
		before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
		require.NoError(t, err)
		after, err := audittest.DecodeAuditData(record.AfterSnapshot)
		require.NoError(t, err)
		require.Equal(t, !enabledAfter, before["enabled"])
		require.Equal(t, enabledAfter, after["enabled"])
		require.Equal(t, destination.ID, after["otel_destination_id"])
		require.Equal(t, "risk_findings", after["data_source"])
	}
}

func TestRouteEnabledCoreReportsUnchangedWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: false, OtelDestinationID: nil})
	require.NoError(t, err)
	routeID := uuid.MustParse(created.ID)
	before := routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID)

	result, err := setRouteEnabled(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID, false)
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Equal(t, before, routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID), "a no-op must not touch updated_at")
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDataExportRoutePause)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRouteEnabledCoreRefusesResumeWithoutDestination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: false, OtelDestinationID: nil})
	require.NoError(t, err)
	routeID := uuid.MustParse(created.ID)
	before := routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID)

	_, err = setRouteEnabled(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID, true)
	require.ErrorIs(t, err, dataexports.ErrRouteDestinationRequired)
	require.Equal(t, before, routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID))
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDataExportRouteResume)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRouteEnabledCoreRefusesResumeToDeletedDestination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	created, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: false, OtelDestinationID: &destination.ID})
	require.NoError(t, err)
	routeID := uuid.MustParse(created.ID)
	// The dashboard refuses to delete a destination a route still names; this
	// reaches the state a route can still be left in by tombstoning the row
	// directly, which is exactly what resume must not paper over.
	_, err = repo.New(ti.conn).SoftDeleteOtelDestination(ctx, repo.SoftDeleteOtelDestinationParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, ID: uuid.MustParse(destination.ID),
	})
	require.NoError(t, err)
	before := routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID)

	_, err = setRouteEnabled(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID, true)
	require.ErrorIs(t, err, dataexports.ErrRouteDestinationInactive)
	require.Equal(t, before, routeRow(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, routeID))
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDataExportRouteResume)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRouteEnabledCoreCannotReachAnotherProjectsRoute(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: false, OtelDestinationID: nil})
	require.NoError(t, err)
	routeID := uuid.MustParse(created.ID)
	otherSlug := "route-enabled-other-" + uuid.NewString()[:8]
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: otherSlug, Slug: otherSlug, OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	_, err = setRouteEnabled(t, ctx, ti, authCtx.ActiveOrganizationID, otherProject.ID, routeID, true)
	require.ErrorIs(t, err, dataexports.ErrRouteNotFound)
	_, err = setRouteEnabled(t, ctx, ti, "org_"+uuid.NewString(), *authCtx.ProjectID, routeID, true)
	require.ErrorIs(t, err, dataexports.ErrRouteNotFound)
}

func TestRouteEnabledCoreRefusesResumeOfEnabledRouteWithUnusableDestination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := repo.New(ti.conn)

	deleted := createDestination(t, ctx, ti, "https://deleted.example.test", "exclude")
	orphaned, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &deleted.ID})
	require.NoError(t, err)
	_, err = queries.SoftDeleteOtelDestination(ctx, repo.SoftDeleteOtelDestinationParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, ID: uuid.MustParse(deleted.ID),
	})
	require.NoError(t, err)
	_, err = setRouteEnabled(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, uuid.MustParse(orphaned.ID), true)
	require.ErrorIs(t, err, dataexports.ErrRouteDestinationInactive, "an enabled route with a deleted destination is not 'already resumed'")

	corrupt := createDestination(t, ctx, ti, "https://corrupt.example.test", "exclude")
	corrupted, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "risk_findings", Enabled: true, OtelDestinationID: &corrupt.ID})
	require.NoError(t, err)
	_, err = queries.UpdateOtelDestination(ctx, repo.UpdateOtelDestinationParams{
		Name: "Test destination", EndpointUrl: "not-a-url", HeadersEncrypted: pgtype.Text{}, SensitiveData: pgtype.Text{String: "exclude", Valid: true},
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, ID: uuid.MustParse(corrupt.ID),
	})
	require.NoError(t, err)
	_, err = setRouteEnabled(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, uuid.MustParse(corrupted.ID), true)
	require.ErrorIs(t, err, dataexports.ErrRouteDestinationInvalid)
	require.Zero(t, auditCountFor(t, ctx, ti, audit.ActionDataExportRouteResume))
}

func auditCountFor(t *testing.T, ctx context.Context, ti *testInstance, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
	require.NoError(t, err)
	return count
}
