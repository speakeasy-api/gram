package dashboards_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/dashboards"
	"github.com/speakeasy-api/gram/server/internal/dashboards/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// inTestTx runs one core in a transaction, committing it when it succeeds.
func inTestTx[T any](t *testing.T, ctx context.Context, ti *testInstance, write func(pgx.Tx) (T, error)) (T, error) {
	t.Helper()
	dbtx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; the cores under test do every read and write.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	result, err := write(dbtx)
	if err != nil {
		return result, err
	}
	require.NoError(t, dbtx.Commit(ctx))
	return result, nil
}

func TestVersionedWritesRefuseAStaleVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created, err := ti.service.CreateDashboard(ctx, createPayload("Usage"))
	require.NoError(t, err)
	read, err := ti.service.ReadDashboard(ctx, created.ID)
	require.NoError(t, err)
	stale := read.Version

	_, err = inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (*gen.Dashboard, error) {
		return ti.service.UpdateDashboardInTx(ctx, dbtx, created.ID, stale, "Renamed", nil)
	})
	require.NoError(t, err)
	fresh, err := ti.service.ReadDashboard(ctx, created.ID)
	require.NoError(t, err)
	require.NotEqual(t, stale, fresh.Version, "a write moves the version")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDashboardUpdate)
	require.NoError(t, err)
	_, err = inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (*gen.Dashboard, error) {
		return ti.service.UpdateDashboardInTx(ctx, dbtx, created.ID, stale, "Again", nil)
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorIs(t, err, dashboards.ErrStaleVersion)
	_, err = inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (*gen.Dashboard, error) {
		return ti.service.SaveDashboardFiltersInTx(ctx, dbtx, created.ID, stale, nil)
	})
	require.ErrorIs(t, err, dashboards.ErrStaleVersion)
	_, err = inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (*gen.Dashboard, error) {
		return ti.service.DuplicateDashboardInTx(ctx, dbtx, created.ID, stale)
	})
	require.ErrorIs(t, err, dashboards.ErrStaleVersion)
	_, err = inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (struct{}, error) {
		return struct{}{}, ti.service.DeleteDashboardInTx(ctx, dbtx, created.ID, stale)
	})
	require.ErrorIs(t, err, dashboards.ErrStaleVersion)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDashboardUpdate)
	require.NoError(t, err)
	require.Equal(t, before, after, "a refused write audits nothing")

	got, err := ti.service.GetDashboard(ctx, getPayload(created.ID))
	require.NoError(t, err)
	require.Equal(t, "Renamed", got.Name)
}

func TestOnlyCardChangesWaitForTheProjectLock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateDashboard(ctx, createPayload("Held"))
	require.NoError(t, err)
	widget := insertWidget(t, ti, "bar", barQuery, barChart)

	// Another write holds the project lock, as a layout change or a widget
	// delete does until it commits.
	holding, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: a transaction held open to hold the project lock another write takes
	require.NoError(t, err)
	t.Cleanup(func() { _ = holding.Rollback(ctx) })
	require.NoError(t, repo.New(holding).LockProjectDashboards(ctx, ti.projectID))

	// A rename and a filter save lock only their dashboard row, so neither
	// waits behind it: one that did would run out of time instead.
	unblocked, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	preset := "30d"
	_, err = ti.service.UpdateDashboard(unblocked, &gen.UpdateDashboardPayload{ID: created.ID, Name: "Renamed", Description: nil, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err, "the rename waited for the project lock")
	_, err = ti.service.SaveDashboardFilters(unblocked, &gen.SaveDashboardFiltersPayload{ID: created.ID, Filters: &gen.DashboardFilters{Range: &gen.DashboardRange{Preset: &preset, From: nil, To: nil, Label: nil}, Values: nil}, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err, "the filter save waited for the project lock")

	// A change to the cards reaches the widgets behind them, so it waits.
	done := make(chan error, 1)
	go func() {
		_, err := ti.service.AddDashboardWidget(ctx, &gen.AddDashboardWidgetPayload{ID: created.ID, WidgetID: widget.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the card change did not wait for the project lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, holding.Rollback(ctx))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the card change did not finish once the lock was released")
	}
	got, err := ti.service.GetDashboard(ctx, getPayload(created.ID))
	require.NoError(t, err)
	require.Equal(t, "Renamed", got.Name)
	require.Len(t, got.Widgets, 1)
}

func TestEditDashboardLayoutAppliesOperationsAsOne(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	bar := insertWidget(t, ti, "Tool calls by user", barQuery, barChart)
	number := insertWidget(t, ti, "Tool calls", numberQuery, numberChart)
	created, err := ti.service.CreateDashboard(ctx, createPayload("Usage"))
	require.NoError(t, err)
	read, err := ti.service.ReadDashboard(ctx, created.ID)
	require.NoError(t, err)

	added, err := inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (*gen.Dashboard, error) {
		return ti.service.EditDashboardLayoutInTx(ctx, dbtx, created.ID, read.Version, []dashboards.LayoutOperation{
			{Kind: dashboards.LayoutAddCard, WidgetID: bar.ID.String(), PlacementID: "", X: nil, Y: nil, W: nil, H: nil},
			{Kind: dashboards.LayoutAddCard, WidgetID: number.ID.String(), PlacementID: "", X: new(6), Y: new(0), W: nil, H: nil},
		})
	})
	require.NoError(t, err)
	require.Len(t, added.Widgets, 2)
	var barCard, numberCard *gen.DashboardPlacement
	for _, card := range added.Widgets {
		if card.WidgetID == bar.ID.String() {
			barCard = card
		} else {
			numberCard = card
		}
	}
	require.Equal(t, gen.DashboardPlacement{ID: barCard.ID, WidgetID: bar.ID.String(), X: 0, Y: 0, W: 6, H: 3}, *barCard)
	require.Equal(t, gen.DashboardPlacement{ID: numberCard.ID, WidgetID: number.ID.String(), X: 6, Y: 0, W: 3, H: 2}, *numberCard)

	detail, err := ti.service.ReadDashboard(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, dashboards.CardWidget{Name: "Tool calls by user", Dataset: "tool_calls", ChartType: "bar"}, detail.Widgets[bar.ID.String()])

	// The two cards trade places in one call: each move alone would overlap
	// the other card, but the final layout does not.
	swapped, err := inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (*gen.Dashboard, error) {
		return ti.service.EditDashboardLayoutInTx(ctx, dbtx, created.ID, detail.Version, []dashboards.LayoutOperation{
			{Kind: dashboards.LayoutMoveCard, WidgetID: "", PlacementID: barCard.ID, X: new(6), Y: new(0), W: new(6), H: new(3)},
			{Kind: dashboards.LayoutMoveCard, WidgetID: "", PlacementID: numberCard.ID, X: new(0), Y: new(0), W: new(3), H: new(2)},
		})
	})
	require.NoError(t, err)
	for _, card := range swapped.Widgets {
		if card.ID == barCard.ID {
			require.Equal(t, 6, card.X)
		} else {
			require.Equal(t, 0, card.X)
		}
	}

	removed, err := inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (*gen.Dashboard, error) {
		return ti.service.EditDashboardLayoutInTx(ctx, dbtx, created.ID, "", []dashboards.LayoutOperation{
			{Kind: dashboards.LayoutRemoveCard, WidgetID: "", PlacementID: numberCard.ID, X: nil, Y: nil, W: nil, H: nil},
		})
	})
	require.NoError(t, err)
	require.Len(t, removed.Widgets, 1)
}

func TestEditDashboardLayoutRefusesBrokenLayouts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	bar := insertWidget(t, ti, "Tool calls by user", barQuery, barChart)
	created, err := ti.service.CreateDashboard(ctx, createPayload("Usage"))
	require.NoError(t, err)
	card := addCard(t, ctx, ti, created.ID, bar.ID)

	for name, tc := range map[string]struct {
		operation dashboards.LayoutOperation
		code      oops.Code
		reason    string
	}{
		"overlap":        {dashboards.LayoutOperation{Kind: dashboards.LayoutAddCard, WidgetID: bar.ID.String(), PlacementID: "", X: new(2), Y: new(0), W: nil, H: nil}, oops.CodeBadRequest, "operations[0] overlaps card"},
		"minimum size":   {dashboards.LayoutOperation{Kind: dashboards.LayoutMoveCard, WidgetID: "", PlacementID: card.ID, X: new(0), Y: new(0), W: new(2), H: new(2)}, oops.CodeBadRequest, "a bar card is at least 4 columns by 3 rows"},
		"past the grid":  {dashboards.LayoutOperation{Kind: dashboards.LayoutMoveCard, WidgetID: "", PlacementID: card.ID, X: new(10), Y: new(0), W: new(6), H: new(3)}, oops.CodeBadRequest, "past the grid's 12 columns"},
		"unknown widget": {dashboards.LayoutOperation{Kind: dashboards.LayoutAddCard, WidgetID: "00000000-0000-0000-0000-000000000001", PlacementID: "", X: nil, Y: nil, W: nil, H: nil}, oops.CodeBadRequest, "is not in this project"},
		"unknown card":   {dashboards.LayoutOperation{Kind: dashboards.LayoutRemoveCard, WidgetID: "", PlacementID: "00000000-0000-0000-0000-000000000001", X: nil, Y: nil, W: nil, H: nil}, oops.CodeNotFound, "is not on this dashboard"},
		"partial move":   {dashboards.LayoutOperation{Kind: dashboards.LayoutMoveCard, WidgetID: "", PlacementID: card.ID, X: new(0), Y: nil, W: nil, H: nil}, oops.CodeBadRequest, "needs x, y, w and h"},
	} {
		_, err := inTestTx(t, ctx, ti, func(dbtx pgx.Tx) (*gen.Dashboard, error) {
			return ti.service.EditDashboardLayoutInTx(ctx, dbtx, created.ID, "", []dashboards.LayoutOperation{tc.operation})
		})
		requireOopsCode(t, err, tc.code)
		require.ErrorContains(t, err, tc.reason, name)
	}

	got, err := ti.service.GetDashboard(ctx, getPayload(created.ID))
	require.NoError(t, err)
	require.Len(t, got.Widgets, 1, "a refused edit changes nothing")
	require.Equal(t, *card, *got.Widgets[0])
}
