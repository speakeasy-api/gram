package dashboards_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/oops"
	widgetsrepo "github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

func TestDashboardFiltersOnAGatedDatasetFollowTheEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	dashboard, err := ti.service.CreateDashboard(ctx, createPayload("signals"))
	require.NoError(t, err)

	// answer_status is a dimension of sensor_signals alone.
	save := &gen.SaveDashboardFiltersPayload{ID: dashboard.ID, Filters: &gen.DashboardFilters{Range: nil, Values: map[string][]string{"answer_status": {"uncertain"}}}, SessionToken: nil, ProjectSlugInput: nil}
	_, err = ti.service.SaveDashboardFilters(ctx, save)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "no dataset has a dimension named that")

	ti.features.signalsIntelligence.Store(true)
	saved, err := ti.service.SaveDashboardFilters(ctx, save)
	require.NoError(t, err)
	require.Equal(t, []string{"uncertain"}, saved.Filters.Values["answer_status"])
}

func TestDuplicatingADashboardWithAGatedWidgetFollowsTheEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	dashboard, err := ti.service.CreateDashboard(ctx, createPayload("signals"))
	require.NoError(t, err)
	widget, err := widgetsrepo.New(ti.conn).CreateWidget(ctx, widgetsrepo.CreateWidgetParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
		Name: "Evaluations", Description: pgtype.Text{String: "", Valid: false}, Dataset: "sensor_evaluations",
		Query: []byte(`{"window":"7d","grain":"none","measures":[{"op":"count","alias":"count"}]}`), Visualization: []byte(numberChart),
	})
	require.NoError(t, err)
	addCard(t, ctx, ti, dashboard.ID, widget.ID)

	_, err = ti.service.DuplicateDashboard(ctx, &gen.DuplicateDashboardPayload{ID: dashboard.ID, SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "unknown_dataset", "a copy cannot carry a widget the organization cannot run")

	ti.features.signalsIntelligence.Store(true)
	copied, err := ti.service.DuplicateDashboard(ctx, &gen.DuplicateDashboardPayload{ID: dashboard.ID, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, copied.Widgets, 1)
}
