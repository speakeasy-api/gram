package dataexports_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func createRouteWithLevel(ctx context.Context, ti *testInstance, source string, level *string) (*gen.DataExportRoute, error) {
	route, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: source, Enabled: false, OtelDestinationID: nil, MinSeverity: level})
	if err != nil {
		return nil, fmt.Errorf("create route: %w", err)
	}
	return route, nil
}

func updateRouteWithLevel(ctx context.Context, ti *testInstance, id, source string, level *string) (*gen.DataExportRoute, error) {
	route, err := ti.service.UpdateRoute(ctx, &gen.UpdateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		ID: id, DataSource: source, Enabled: false, OtelDestinationID: nil, MinSeverity: level})
	if err != nil {
		return nil, fmt.Errorf("update route: %w", err)
	}
	return route, nil
}

// settingsRows counts the route's stored settings: the inventory query reads them with a LEFT JOIN.
func settingsRows(t *testing.T, ctx context.Context, ti *testInstance, routeID string) int {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := repo.New(ti.conn).ListDataExportRoutesWithProjectMetadata(ctx, repo.ListDataExportRoutesWithProjectMetadataParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		ResultLimit:    100,
	})
	require.NoError(t, err)
	count := 0
	for _, row := range rows {
		if row.ID.String() == routeID && row.MinSeverity.Valid {
			count++
		}
	}
	return count
}

func otherProjectContext(t *testing.T, ctx context.Context, ti *testInstance) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "data-exports-other-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	other := *authCtx
	other.ProjectID = &project.ID
	other.ProjectSlug = &slug
	return contextvalues.SetAuthContext(ctx, &other)
}

func TestDefaultMCPConnectionLogMinSeverityMatchesColumnDefault(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile(filepath.Join("..", "..", "database", "schema.sql"))
	require.NoError(t, err)
	table := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS mcp_connection_log_routes \((.*?)\n\);`).FindSubmatch(schema)
	require.NotNil(t, table, "mcp_connection_log_routes is in schema.sql")
	column := regexp.MustCompile(`min_severity TEXT NOT NULL DEFAULT '([a-z]+)'`).FindSubmatch(table[1])
	require.NotNil(t, column, "min_severity has a default")
	require.Equal(t, dataexports.DefaultMCPConnectionLogMinSeverity, string(column[1]))
}

func TestNormalizeMinSeverity(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"warn", "WARN", " warn ", "Warn\n"} {
		level, err := dataexports.NormalizeMinSeverity(dataexports.DataSourceMCPConnectionLogs, value)
		require.NoError(t, err, value)
		require.Equal(t, dataexports.MinSeverityWarn, level, value)
	}
	for _, value := range []string{"", " ", "debug", "warning"} {
		_, err := dataexports.NormalizeMinSeverity(dataexports.DataSourceMCPConnectionLogs, value)
		require.ErrorContains(t, err, "must be info, warn or error", value)
	}
	_, err := dataexports.NormalizeMinSeverity(dataexports.DataSourceRiskFindings, dataexports.MinSeverityWarn)
	require.ErrorContains(t, err, "only supported on mcp_connection_logs routes")
}

func TestRouteMinSeverityIsStoredNormalized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := createRouteWithLevel(ctx, ti, dataexports.DataSourceMCPConnectionLogs, new(" WARN "))
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityWarn, *created.MinSeverity)

	listed, err := ti.service.ListRoutes(ctx, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Routes, 1)
	require.Equal(t, dataexports.MinSeverityWarn, *listed.Routes[0].MinSeverity)
}

func TestRouteMinSeverityDefaultsToErrorWithoutARow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := createRouteWithLevel(ctx, ti, dataexports.DataSourceMCPConnectionLogs, nil)
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityError, *created.MinSeverity)
	require.Zero(t, settingsRows(t, ctx, ti, created.ID))

	listed, err := ti.service.ListRoutes(ctx, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Routes, 1)
	require.Equal(t, dataexports.MinSeverityError, *listed.Routes[0].MinSeverity)
}

func TestRouteMinSeverityIsSetUpdatedAndKept(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := createRouteWithLevel(ctx, ti, dataexports.DataSourceMCPConnectionLogs, new(dataexports.MinSeverityWarn))
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityWarn, *created.MinSeverity)
	require.Equal(t, 1, settingsRows(t, ctx, ti, created.ID))

	updated, err := updateRouteWithLevel(ctx, ti, created.ID, dataexports.DataSourceMCPConnectionLogs, new(dataexports.MinSeverityInfo))
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityInfo, *updated.MinSeverity)
	require.Equal(t, 1, settingsRows(t, ctx, ti, created.ID), "the upsert replaces the row")

	auditRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDataExportRouteUpdate)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(auditRecord.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(auditRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityWarn, before["min_severity"])
	require.Equal(t, dataexports.MinSeverityInfo, after["min_severity"])

	kept, err := updateRouteWithLevel(ctx, ti, created.ID, dataexports.DataSourceMCPConnectionLogs, nil)
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityInfo, *kept.MinSeverity, "omitting the level keeps it")

	listed, err := ti.service.ListRoutes(ctx, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Routes, 1)
	require.Equal(t, dataexports.MinSeverityInfo, *listed.Routes[0].MinSeverity)

	forOrg, err := ti.service.ListForOrg(ctx, &gen.ListForOrgPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, forOrg.Routes, 1)
	require.Equal(t, dataexports.MinSeverityInfo, *forOrg.Routes[0].MinSeverity)
}

func TestRouteMinSeverityIsRejectedOutsideConnectionLogs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, err := createRouteWithLevel(ctx, ti, dataexports.DataSourceProductTelemetry, new(dataexports.MinSeverityWarn))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "only supported on mcp_connection_logs routes")

	_, err = createRouteWithLevel(ctx, ti, dataexports.DataSourceMCPConnectionLogs, new("debug"))
	requireOopsCode(t, err, oops.CodeInvalid)

	telemetry, err := createRouteWithLevel(ctx, ti, dataexports.DataSourceProductTelemetry, nil)
	require.NoError(t, err)
	require.Nil(t, telemetry.MinSeverity)
	_, err = updateRouteWithLevel(ctx, ti, telemetry.ID, dataexports.DataSourceProductTelemetry, new(dataexports.MinSeverityError))
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRouteSourceChangeRemovesConnectionLogSettings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := createRouteWithLevel(ctx, ti, dataexports.DataSourceMCPConnectionLogs, new(dataexports.MinSeverityWarn))
	require.NoError(t, err)

	moved, err := updateRouteWithLevel(ctx, ti, created.ID, dataexports.DataSourceRiskFindings, nil)
	require.NoError(t, err)
	require.Equal(t, dataexports.DataSourceRiskFindings, moved.DataSource)
	require.Nil(t, moved.MinSeverity)
	require.Zero(t, settingsRows(t, ctx, ti, created.ID))

	back, err := updateRouteWithLevel(ctx, ti, created.ID, dataexports.DataSourceMCPConnectionLogs, nil)
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityError, *back.MinSeverity, "the old level does not come back")
}

func TestRouteMinSeverityStorageIsTenantScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := createRouteWithLevel(ctx, ti, dataexports.DataSourceMCPConnectionLogs, new(dataexports.MinSeverityWarn))
	require.NoError(t, err)
	routeID := uuid.MustParse(created.ID)

	otherCtx := otherProjectContext(t, ctx, ti)
	otherAuthCtx, ok := contextvalues.GetAuthContext(otherCtx)
	require.True(t, ok)
	queries := repo.New(ti.conn)

	for _, tenant := range []struct {
		name           string
		organizationID string
		projectID      uuid.UUID
	}{
		{name: "other project", organizationID: authCtx.ActiveOrganizationID, projectID: *otherAuthCtx.ProjectID},
		{name: "other organization", organizationID: "org-other-" + uuid.NewString()[:8], projectID: *authCtx.ProjectID},
	} {
		_, err = queries.UpsertMCPConnectionLogRouteSettings(ctx, repo.UpsertMCPConnectionLogRouteSettingsParams{
			OrganizationID: tenant.organizationID, ProjectID: tenant.projectID, MinSeverity: dataexports.MinSeverityInfo, DataExportRouteID: routeID,
		})
		require.ErrorIs(t, err, pgx.ErrNoRows, tenant.name)

		_, err = queries.GetMCPConnectionLogRouteMinSeverity(ctx, repo.GetMCPConnectionLogRouteMinSeverityParams{
			DefaultMinSeverity: dataexports.DefaultMCPConnectionLogMinSeverity, OrganizationID: tenant.organizationID, ProjectID: tenant.projectID, DataExportRouteID: routeID,
		})
		require.ErrorIs(t, err, pgx.ErrNoRows, tenant.name)

		routes, err := queries.ListDataExportRoutes(ctx, repo.ListDataExportRoutesParams{
			OrganizationID: tenant.organizationID, ProjectID: tenant.projectID,
		})
		require.NoError(t, err)
		require.Empty(t, routes, tenant.name)

		require.NoError(t, queries.DeleteMCPConnectionLogRouteSettings(ctx, repo.DeleteMCPConnectionLogRouteSettingsParams{
			OrganizationID: tenant.organizationID, ProjectID: tenant.projectID, DataExportRouteID: routeID,
		}))
		require.Equal(t, 1, settingsRows(t, ctx, ti, created.ID), tenant.name)
	}

	_, err = updateRouteWithLevel(otherCtx, ti, created.ID, dataexports.DataSourceMCPConnectionLogs, new(dataexports.MinSeverityInfo))
	requireOopsCode(t, err, oops.CodeNotFound)

	level, err := queries.GetMCPConnectionLogRouteMinSeverity(ctx, repo.GetMCPConnectionLogRouteMinSeverityParams{
		DefaultMinSeverity: dataexports.DefaultMCPConnectionLogMinSeverity, OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, DataExportRouteID: routeID,
	})
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityWarn, level)
}

func TestRouteMinSeverityIgnoresDeletedRoutes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := createRouteWithLevel(ctx, ti, dataexports.DataSourceMCPConnectionLogs, new(dataexports.MinSeverityWarn))
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteRoute(ctx, &gen.DeleteRoutePayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))

	_, err = repo.New(ti.conn).UpsertMCPConnectionLogRouteSettings(ctx, repo.UpsertMCPConnectionLogRouteSettingsParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, MinSeverity: dataexports.MinSeverityInfo, DataExportRouteID: uuid.MustParse(created.ID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
