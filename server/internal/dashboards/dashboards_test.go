package dashboards_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	widgetsrepo "github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

// A bar chart over sessions, as the widgets service would save it.
const barQuery = `{"window":"7d","grain":"day","dimensions":["user"],"measures":[{"op":"count","alias":"count"}],"limit":1000}`
const barChart = `{"type":"bar","options":{}}`

// A number tile over sessions.
const numberQuery = `{"window":"7d","grain":"none","measures":[{"op":"count","alias":"count"}]}`
const numberChart = `{"type":"number","options":{}}`

// insertWidget writes a widget straight to the database, as the widgets
// service would store it, owned by the test's admin user.
func insertWidget(t *testing.T, ti *testInstance, name, query, visualization string) widgetsrepo.Widget {
	t.Helper()
	row, err := widgetsrepo.New(ti.conn).CreateWidget(t.Context(), widgetsrepo.CreateWidgetParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
		Name: name, Description: pgtype.Text{String: "", Valid: false}, Dataset: "sessions", Query: []byte(query), Visualization: []byte(visualization),
	})
	require.NoError(t, err)
	return row
}

func createPayload(name string) *gen.CreateDashboardPayload {
	return &gen.CreateDashboardPayload{Name: name, Description: nil, SessionToken: nil, ProjectSlugInput: nil}
}

func getPayload(id string) *gen.GetDashboardPayload {
	return &gen.GetDashboardPayload{ID: id, SessionToken: nil, ProjectSlugInput: nil}
}

func placement(id *string, widgetID string, x, y, w, h int) *gen.PlacementInput {
	return &gen.PlacementInput{ID: id, WidgetID: widgetID, X: x, Y: y, W: w, H: h}
}

func layoutPayload(id string, placements ...*gen.PlacementInput) *gen.SaveDashboardLayoutPayload {
	return &gen.SaveDashboardLayoutPayload{ID: id, Placements: placements, SessionToken: nil, ProjectSlugInput: nil}
}

func TestCreateDashboard(t *testing.T) {
	t.Parallel()

	t.Run("it makes an empty dashboard, records the creator, and audits it", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDashboardCreate)
		require.NoError(t, err)

		description := "What the team looks at on Monday"
		payload := createPayload("Weekly review")
		payload.Description = &description
		created, err := ti.service.CreateDashboard(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, "Weekly review", created.Name)
		require.Equal(t, &description, created.Description)
		require.Equal(t, ti.projectID.String(), created.ProjectID)
		require.NotNil(t, created.CreatedByUserID)
		require.Equal(t, ti.userID, *created.CreatedByUserID)
		require.Empty(t, created.Widgets)
		require.Nil(t, created.Filters.Range, "a fresh dashboard opens on the page's default range")
		require.Empty(t, created.Filters.Values)

		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDashboardCreate)
		require.NoError(t, err)
		require.Equal(t, before+1, after)
	})

	t.Run("it needs a signed-in member", func(t *testing.T) {
		t.Parallel()
		_, ti := newTestService(t)
		_, err := ti.service.CreateDashboard(t.Context(), createPayload("nobody's"))
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}

func TestDashboardDetailsAreChecked(t *testing.T) {
	t.Parallel()

	t.Run("it refuses a blank or overlong name and an overlong description, on creation and on update", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)

		_, err := ti.service.CreateDashboard(ctx, createPayload("   "))
		require.ErrorContains(t, err, "a dashboard needs a name")
		_, err = ti.service.CreateDashboard(ctx, createPayload(strings.Repeat("é", 201)))
		require.ErrorContains(t, err, "a dashboard name is at most 200 characters")
		long := strings.Repeat("é", 2001)
		_, err = ti.service.CreateDashboard(ctx, &gen.CreateDashboardPayload{Name: "ok", Description: &long, SessionToken: nil, ProjectSlugInput: nil})
		require.ErrorContains(t, err, "a dashboard description is at most 2000 characters")

		created, err := ti.service.CreateDashboard(ctx, createPayload(strings.Repeat("é", 200)))
		require.NoError(t, err)
		_, err = ti.service.UpdateDashboard(ctx, &gen.UpdateDashboardPayload{ID: created.ID, Name: strings.Repeat("x", 201), Description: nil, SessionToken: nil, ProjectSlugInput: nil})
		require.ErrorContains(t, err, "a dashboard name is at most 200 characters")
		_, err = ti.service.UpdateDashboard(ctx, &gen.UpdateDashboardPayload{ID: created.ID, Name: "ok", Description: &long, SessionToken: nil, ProjectSlugInput: nil})
		require.ErrorContains(t, err, "a dashboard description is at most 2000 characters")
	})
}

func TestListAndGetDashboards(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	first, err := ti.service.CreateDashboard(ctx, createPayload("first"))
	require.NoError(t, err)
	second, err := ti.service.CreateDashboard(ctx, createPayload("second"))
	require.NoError(t, err)
	widget := insertWidget(t, ti, "Sessions", numberQuery, numberChart)
	_, err = ti.service.AddDashboardWidget(ctx, &gen.AddDashboardWidgetPayload{ID: first.ID, WidgetID: widget.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	listed, err := ti.service.ListDashboards(ctx, &gen.ListDashboardsPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Dashboards, 2)
	require.Equal(t, first.ID, listed.Dashboards[0].ID, "a card added just now moves the dashboard to the top")
	require.Len(t, listed.Dashboards[0].Widgets, 1, "the list carries each dashboard's cards")
	require.Equal(t, second.ID, listed.Dashboards[1].ID)
	require.Empty(t, listed.Dashboards[1].Widgets)

	got, err := ti.service.GetDashboard(ctx, getPayload(first.ID))
	require.NoError(t, err)
	require.Len(t, got.Widgets, 1)
	require.Equal(t, widget.ID.String(), got.Widgets[0].WidgetID)

	// A deleted widget leaves no card behind.
	_, err = widgetsrepo.New(ti.conn).DeleteWidget(ctx, widgetsrepo.DeleteWidgetParams{ProjectID: ti.projectID, ID: widget.ID})
	require.NoError(t, err)
	got, err = ti.service.GetDashboard(ctx, getPayload(first.ID))
	require.NoError(t, err)
	require.Empty(t, got.Widgets)

	_, err = ti.service.GetDashboard(ctx, getPayload(uuid.NewString()))
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.ListDashboards(t.Context(), &gen.ListDashboardsPayload{SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestUpdateDashboard(t *testing.T) {
	t.Parallel()

	t.Run("it renames the dashboard and audits before and after", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateDashboard(ctx, createPayload("before"))
		require.NoError(t, err)

		updated, err := ti.service.UpdateDashboard(ctx, &gen.UpdateDashboardPayload{ID: created.ID, Name: "after", Description: nil, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, "after", updated.Name)

		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDashboardUpdate)
		require.NoError(t, err)
		snapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
		require.NoError(t, err)
		require.Equal(t, "before", snapshot["Name"])
	})

	t.Run("its creator can change it with membership alone, another member cannot without project write", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		ownerCtx := asMember(t, ctx, ti, "user_owner_"+uuid.NewString())
		created, err := ti.service.CreateDashboard(ownerCtx, createPayload("mine"))
		require.NoError(t, err)

		_, err = ti.service.UpdateDashboard(ownerCtx, &gen.UpdateDashboardPayload{ID: created.ID, Name: "still mine", Description: nil, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)

		otherCtx := asMember(t, ctx, ti, "user_other_"+uuid.NewString())
		_, err = ti.service.UpdateDashboard(otherCtx, &gen.UpdateDashboardPayload{ID: created.ID, Name: "hijacked", Description: nil, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeForbidden)

		writerCtx := asMember(t, ctx, ti, "user_writer_"+uuid.NewString(), authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
		updated, err := ti.service.UpdateDashboard(writerCtx, &gen.UpdateDashboardPayload{ID: created.ID, Name: "edited", Description: nil, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, "edited", updated.Name)
	})

	t.Run("it reports an unknown or deleted dashboard as not found", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.UpdateDashboard(ctx, &gen.UpdateDashboardPayload{ID: uuid.NewString(), Name: "x", Description: nil, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)

		created, err := ti.service.CreateDashboard(ctx, createPayload("gone"))
		require.NoError(t, err)
		require.NoError(t, ti.service.DeleteDashboard(ctx, &gen.DeleteDashboardPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
		_, err = ti.service.UpdateDashboard(ctx, &gen.UpdateDashboardPayload{ID: created.ID, Name: "x", Description: nil, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}

func TestSaveDashboardLayout(t *testing.T) {
	t.Parallel()

	t.Run("it adds, moves and removes cards, keeping each card's id across saves", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		dashboard, err := ti.service.CreateDashboard(ctx, createPayload("layout"))
		require.NoError(t, err)
		tile := insertWidget(t, ti, "Sessions", numberQuery, numberChart)
		chart := insertWidget(t, ti, "Sessions by user", barQuery, barChart)

		laid, err := ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID,
			placement(nil, tile.ID.String(), 0, 0, 3, 2),
			placement(nil, chart.ID.String(), 3, 0, 9, 3),
		))
		require.NoError(t, err)
		require.Len(t, laid.Widgets, 2)
		tileCard, chartCard := laid.Widgets[0], laid.Widgets[1]
		require.Equal(t, tile.ID.String(), tileCard.WidgetID)
		require.Equal(t, 9, chartCard.W)

		// Moving keeps the ids; leaving the chart out removes it.
		moved, err := ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID,
			placement(&tileCard.ID, tile.ID.String(), 6, 4, 4, 2),
		))
		require.NoError(t, err)
		require.Len(t, moved.Widgets, 1)
		require.Equal(t, tileCard.ID, moved.Widgets[0].ID)
		require.Equal(t, 6, moved.Widgets[0].X)
		require.Equal(t, 4, moved.Widgets[0].Y)

		// The same widget may be on the dashboard twice.
		twice, err := ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID,
			placement(&tileCard.ID, tile.ID.String(), 0, 0, 3, 2),
			placement(nil, tile.ID.String(), 3, 0, 3, 2),
		))
		require.NoError(t, err)
		require.Len(t, twice.Widgets, 2)

		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDashboardUpdate)
		require.NoError(t, err)
		require.NotEmpty(t, record.BeforeSnapshot)
	})

	t.Run("it refuses a card that is too small, runs past the grid, or points nowhere", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		dashboard, err := ti.service.CreateDashboard(ctx, createPayload("layout"))
		require.NoError(t, err)
		tile := insertWidget(t, ti, "Sessions", numberQuery, numberChart)
		chart := insertWidget(t, ti, "Sessions by user", barQuery, barChart)

		_, err = ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID, placement(nil, chart.ID.String(), 0, 0, 3, 3)))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "placements[0]: a bar card is at least 4 columns by 3 rows")

		_, err = ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID, placement(nil, tile.ID.String(), 0, 0, 2, 1)))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "a number card is at least 2 columns by 2 rows")

		_, err = ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID, placement(nil, tile.ID.String(), 10, 0, 3, 2)))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "past the grid's 12 columns")

		_, err = ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID, placement(nil, uuid.NewString(), 0, 0, 3, 2)))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "is not in this project")

		unknown := uuid.NewString()
		_, err = ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID, placement(&unknown, tile.ID.String(), 0, 0, 3, 2)))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "no card")

		// A refused layout changes nothing.
		got, err := ti.service.GetDashboard(ctx, getPayload(dashboard.ID))
		require.NoError(t, err)
		require.Empty(t, got.Widgets)
	})

	t.Run("a card keeps its widget: pointing it at another is refused", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		dashboard, err := ti.service.CreateDashboard(ctx, createPayload("layout"))
		require.NoError(t, err)
		tile := insertWidget(t, ti, "Sessions", numberQuery, numberChart)
		other := insertWidget(t, ti, "Other", numberQuery, numberChart)
		laid, err := ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID, placement(nil, tile.ID.String(), 0, 0, 3, 2)))
		require.NoError(t, err)

		_, err = ti.service.SaveDashboardLayout(ctx, layoutPayload(dashboard.ID, placement(&laid.Widgets[0].ID, other.ID.String(), 0, 0, 3, 2)))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "shows another widget")
	})

	t.Run("another member cannot lay out someone else's dashboard", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		dashboard, err := ti.service.CreateDashboard(ctx, createPayload("theirs"))
		require.NoError(t, err)
		otherCtx := asMember(t, ctx, ti, "user_other_"+uuid.NewString())
		_, err = ti.service.SaveDashboardLayout(otherCtx, layoutPayload(dashboard.ID))
		requireOopsCode(t, err, oops.CodeForbidden)
	})
}

func TestAddAndRemoveDashboardWidget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	dashboard, err := ti.service.CreateDashboard(ctx, createPayload("cards"))
	require.NoError(t, err)
	tile := insertWidget(t, ti, "Sessions", numberQuery, numberChart)
	chart := insertWidget(t, ti, "Sessions by user", barQuery, barChart)

	withTile, err := ti.service.AddDashboardWidget(ctx, &gen.AddDashboardWidgetPayload{ID: dashboard.ID, WidgetID: tile.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, withTile.Widgets, 1)
	require.Equal(t, []int{0, 0, 3, 2}, []int{withTile.Widgets[0].X, withTile.Widgets[0].Y, withTile.Widgets[0].W, withTile.Widgets[0].H}, "a number tile opens a quarter wide")

	withChart, err := ti.service.AddDashboardWidget(ctx, &gen.AddDashboardWidgetPayload{ID: dashboard.ID, WidgetID: chart.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, withChart.Widgets, 2)
	added := withChart.Widgets[1]
	require.Equal(t, []int{0, 2, 6, 3}, []int{added.X, added.Y, added.W, added.H}, "a chart opens half wide, under what is there")

	_, err = ti.service.AddDashboardWidget(ctx, &gen.AddDashboardWidgetPayload{ID: dashboard.ID, WidgetID: uuid.NewString(), SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	removed, err := ti.service.RemoveDashboardWidget(ctx, &gen.RemoveDashboardWidgetPayload{ID: dashboard.ID, PlacementID: withTile.Widgets[0].ID, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, removed.Widgets, 1)
	require.Equal(t, added.ID, removed.Widgets[0].ID)

	_, err = ti.service.RemoveDashboardWidget(ctx, &gen.RemoveDashboardWidgetPayload{ID: dashboard.ID, PlacementID: withTile.Widgets[0].ID, SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	// The widget itself is untouched.
	_, err = widgetsrepo.New(ti.conn).GetWidget(ctx, widgetsrepo.GetWidgetParams{ProjectID: ti.projectID, ID: tile.ID})
	require.NoError(t, err)
}

func TestSaveDashboardFilters(t *testing.T) {
	t.Parallel()

	t.Run("it stores a preset, or an absolute range with its label, and the values picked", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		dashboard, err := ti.service.CreateDashboard(ctx, createPayload("filters"))
		require.NoError(t, err)

		preset := "30d"
		saved, err := ti.service.SaveDashboardFilters(ctx, &gen.SaveDashboardFiltersPayload{ID: dashboard.ID, Filters: &gen.DashboardFilters{
			Range:  &gen.DashboardRange{Preset: &preset, From: nil, To: nil, Label: nil},
			Values: map[string][]string{"user": {"ann", "bob"}},
		}, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, &preset, saved.Filters.Range.Preset)
		require.Equal(t, []string{"ann", "bob"}, saved.Filters.Values["user"])

		got, err := ti.service.GetDashboard(ctx, getPayload(dashboard.ID))
		require.NoError(t, err)
		require.Equal(t, saved.Filters, got.Filters, "what was saved is what is read back")

		from, to, label := "2026-09-01T00:00:00Z", "2026-09-08T00:00:00Z", "First week of September"
		saved, err = ti.service.SaveDashboardFilters(ctx, &gen.SaveDashboardFiltersPayload{ID: dashboard.ID, Filters: &gen.DashboardFilters{
			Range:  &gen.DashboardRange{Preset: nil, From: &from, To: &to, Label: &label},
			Values: map[string][]string{},
		}, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, &label, saved.Filters.Range.Label)
		require.Empty(t, saved.Filters.Values)
	})

	t.Run("it refuses a range that is not one a dashboard can open on", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		dashboard, err := ti.service.CreateDashboard(ctx, createPayload("filters"))
		require.NoError(t, err)
		save := func(r *gen.DashboardRange, values map[string][]string) error {
			_, err := ti.service.SaveDashboardFilters(ctx, &gen.SaveDashboardFiltersPayload{ID: dashboard.ID, Filters: &gen.DashboardFilters{Range: r, Values: values}, SessionToken: nil, ProjectSlugInput: nil})
			if err != nil {
				return fmt.Errorf("save filters: %w", err)
			}
			return nil
		}
		year, from, to := "1y", "2026-09-08T00:00:00Z", "2026-09-01T00:00:00Z"

		err = save(&gen.DashboardRange{Preset: &year, From: nil, To: nil, Label: nil}, nil)
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, `"1y"`)

		err = save(&gen.DashboardRange{Preset: nil, From: &from, To: &to, Label: nil}, nil)
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "from must be before to")

		err = save(&gen.DashboardRange{Preset: &year, From: &from, To: nil, Label: nil}, nil)
		requireOopsCode(t, err, oops.CodeBadRequest)

		tooMany := make([]string, 101)
		for i := range tooMany {
			tooMany[i] = "v"
		}
		err = save(nil, map[string][]string{"user": tooMany})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "at most 100 values")
	})
}

func TestDuplicateDashboard(t *testing.T) {
	t.Parallel()

	t.Run("it copies the dashboard and every widget on it into ones the caller owns", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		source, err := ti.service.CreateDashboard(ctx, createPayload("Weekly review"))
		require.NoError(t, err)
		tile := insertWidget(t, ti, "Sessions", numberQuery, numberChart)
		chart := insertWidget(t, ti, "Sessions by user", barQuery, barChart)
		// The tile twice, so one widget copy serves two cards.
		laid, err := ti.service.SaveDashboardLayout(ctx, layoutPayload(source.ID,
			placement(nil, tile.ID.String(), 0, 0, 3, 2),
			placement(nil, tile.ID.String(), 3, 0, 3, 2),
			placement(nil, chart.ID.String(), 0, 2, 12, 3),
		))
		require.NoError(t, err)
		preset := "30d"
		_, err = ti.service.SaveDashboardFilters(ctx, &gen.SaveDashboardFiltersPayload{ID: source.ID, Filters: &gen.DashboardFilters{Range: &gen.DashboardRange{Preset: &preset, From: nil, To: nil, Label: nil}, Values: map[string][]string{}}, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)

		memberID := "user_member_" + uuid.NewString()
		memberCtx := asMember(t, ctx, ti, memberID)
		widgetsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetCreate)
		require.NoError(t, err)

		copied, err := ti.service.DuplicateDashboard(memberCtx, &gen.DuplicateDashboardPayload{ID: source.ID, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.NotEqual(t, source.ID, copied.ID)
		require.Equal(t, "Weekly review (copy)", copied.Name)
		require.Equal(t, memberID, *copied.CreatedByUserID)
		require.Equal(t, &preset, copied.Filters.Range.Preset, "the saved filters come along")
		require.Len(t, copied.Widgets, 3)

		// Every card points at a new widget, and the two tile cards share
		// one copy.
		sourceWidgetIDs := map[string]bool{tile.ID.String(): true, chart.ID.String(): true}
		copiedWidgetIDs := map[string]bool{}
		for i, card := range copied.Widgets {
			require.False(t, sourceWidgetIDs[card.WidgetID], "card %d still points at the original widget", i)
			copiedWidgetIDs[card.WidgetID] = true
			require.Equal(t, []int{laid.Widgets[i].X, laid.Widgets[i].Y, laid.Widgets[i].W, laid.Widgets[i].H}, []int{card.X, card.Y, card.W, card.H}, "the layout is kept")
		}
		require.Len(t, copiedWidgetIDs, 2)
		widgetsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetCreate)
		require.NoError(t, err)
		require.Equal(t, widgetsBefore+2, widgetsAfter, "each widget copy is audited as a widget creation")

		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDashboardCreate)
		require.NoError(t, err)
		metadata, err := audittest.DecodeAuditData(record.Metadata)
		require.NoError(t, err)
		require.Equal(t, "dashboard:"+source.ID, metadata["duplicated_from"])

		// The copy is the member's: they can delete it with membership
		// alone, and the original and its widgets stay.
		require.NoError(t, ti.service.DeleteDashboard(memberCtx, &gen.DeleteDashboardPayload{ID: copied.ID, SessionToken: nil, ProjectSlugInput: nil}))
		got, err := ti.service.GetDashboard(ctx, getPayload(source.ID))
		require.NoError(t, err)
		require.Len(t, got.Widgets, 3)
	})

	t.Run("it keeps a long name within the limit", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		source, err := ti.service.CreateDashboard(ctx, createPayload(strings.Repeat("é", 200)))
		require.NoError(t, err)
		copied, err := ti.service.DuplicateDashboard(ctx, &gen.DuplicateDashboardPayload{ID: source.ID, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(copied.Name, " (copy)"))
		require.Len(t, []rune(copied.Name), 200)
	})

	t.Run("it refuses while a widget on it is broken, naming the widget", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		source, err := ti.service.CreateDashboard(ctx, createPayload("broken"))
		require.NoError(t, err)
		broken := insertWidget(t, ti, "Sessions by department", `{"window":"7d","grain":"none","dimensions":["department"],"measures":[{"op":"count","alias":"count"}]}`, `{"type":"table","options":{}}`)
		_, err = ti.service.AddDashboardWidget(ctx, &gen.AddDashboardWidgetPayload{ID: source.ID, WidgetID: broken.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err, "a broken widget can still sit on a dashboard; its card says why")

		_, err = ti.service.DuplicateDashboard(ctx, &gen.DuplicateDashboardPayload{ID: source.ID, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, `"Sessions by department"`)
		require.ErrorContains(t, err, "department")
	})

	t.Run("it reports an unknown dashboard", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.DuplicateDashboard(ctx, &gen.DuplicateDashboardPayload{ID: uuid.NewString(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}

func TestDeleteDashboard(t *testing.T) {
	t.Parallel()

	t.Run("its creator can delete it, and its widgets stay", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		memberCtx := asMember(t, ctx, ti, "user_owner_"+uuid.NewString())
		created, err := ti.service.CreateDashboard(memberCtx, createPayload("mine"))
		require.NoError(t, err)
		tile := insertWidget(t, ti, "Sessions", numberQuery, numberChart)
		_, err = ti.service.AddDashboardWidget(memberCtx, &gen.AddDashboardWidgetPayload{ID: created.ID, WidgetID: tile.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)

		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDashboardDelete)
		require.NoError(t, err)
		require.NoError(t, ti.service.DeleteDashboard(memberCtx, &gen.DeleteDashboardPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDashboardDelete)
		require.NoError(t, err)
		require.Equal(t, before+1, after)

		listed, err := ti.service.ListDashboards(ctx, &gen.ListDashboardsPayload{SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Empty(t, listed.Dashboards)
		_, err = widgetsrepo.New(ti.conn).GetWidget(ctx, widgetsrepo.GetWidgetParams{ProjectID: ti.projectID, ID: tile.ID})
		require.NoError(t, err, "deleting a dashboard leaves its widgets")
	})

	t.Run("another member cannot delete it without project write", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateDashboard(ctx, createPayload("theirs"))
		require.NoError(t, err)

		otherCtx := asMember(t, ctx, ti, "user_other_"+uuid.NewString())
		err = ti.service.DeleteDashboard(otherCtx, &gen.DeleteDashboardPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeForbidden)

		writerCtx := asMember(t, ctx, ti, "user_writer_"+uuid.NewString(), authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
		require.NoError(t, ti.service.DeleteDashboard(writerCtx, &gen.DeleteDashboardPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
	})
}
