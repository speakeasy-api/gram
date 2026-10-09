package widgets_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	widgetssrv "github.com/speakeasy-api/gram/server/gen/http/widgets/server"
	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	dashboardsrepo "github.com/speakeasy-api/gram/server/internal/dashboards/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/widgets"
	widgetsrepo "github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

func validQuery() map[string]any {
	return map[string]any{
		"window":     "7d",
		"grain":      "day",
		"dimensions": []any{"user"},
		"measures":   []any{map[string]any{"op": "count"}, map[string]any{"op": "sum", "field": "tool_call_count", "alias": "tool_calls"}},
		"filters":    []any{map[string]any{"field": "surface", "operator": "in", "values": []any{"claude-code"}}},
		"limit":      50,
	}
}

func barChart() map[string]any {
	return chart(widgets.ChartBar)
}

// chart is a visualization of the given type with no options.
func chart(chartType widgets.ChartType) map[string]any {
	return map[string]any{"type": string(chartType), "options": map[string]any{}}
}

func createPayload(name string, query, visualization map[string]any) *gen.CreateWidgetPayload {
	return &gen.CreateWidgetPayload{Name: name, Description: nil, Dataset: "sessions", Query: query, Visualization: visualization, SessionToken: nil, ProjectSlugInput: nil}
}

func TestCreateWidget(t *testing.T) {
	t.Parallel()

	t.Run("it saves a valid widget, records the creator, and audits it", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetCreate)
		require.NoError(t, err)

		description := "Who is calling tools"
		payload := createPayload("Tool calls by user", validQuery(), barChart())
		payload.Description = &description
		created, err := ti.service.CreateWidget(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, "Tool calls by user", created.Name)
		require.Equal(t, &description, created.Description)
		require.Equal(t, "sessions", created.Dataset)
		require.Equal(t, ti.projectID.String(), created.ProjectID)
		require.NotNil(t, created.CreatedByUserID)
		require.Equal(t, ti.userID, *created.CreatedByUserID)
		require.Equal(t, string(widgets.ChartBar), created.Visualization["type"])
		require.Equal(t, "day", created.Query["grain"])
		require.Nil(t, created.InvalidReason)

		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetCreate)
		require.NoError(t, err)
		require.Equal(t, before+1, after)
	})

	t.Run("it keeps visualization options it does not interpret", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		stacked := map[string]any{"type": string(widgets.ChartBar), "options": map[string]any{"stack": "normal", "series_limit": 12}}
		created, err := ti.service.CreateWidget(ctx, createPayload("stacked", validQuery(), stacked))
		require.NoError(t, err)
		options, ok := created.Visualization["options"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "normal", options["stack"])
	})

	t.Run("it accepts a chart type the server does not know", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.CreateWidget(ctx, createPayload("new chart", validQuery(), map[string]any{"type": "heatmap"}))
		require.NoError(t, err, "the client owns the chart vocabulary")
	})

	t.Run("it saves a distinct count over a dimension and refuses one over a measure", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		query := validQuery()
		query["measures"] = []any{map[string]any{"op": "count_distinct", "field": "user", "alias": "people"}}
		created, err := ti.service.CreateWidget(ctx, createPayload("people", query, barChart()))
		require.NoError(t, err)
		require.Nil(t, created.InvalidReason)

		query = validQuery()
		query["measures"] = []any{map[string]any{"op": "count_distinct", "field": "tool_call_count"}}
		_, err = ti.service.CreateWidget(ctx, createPayload("distinct counts", query, barChart()))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unsupported_aggregation")
		require.ErrorContains(t, err, "measures[0].op")
	})

	t.Run("it rejects a query the catalog cannot plan, naming the field", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		query := validQuery()
		query["dimensions"] = []any{"department"}
		_, err := ti.service.CreateWidget(ctx, createPayload("bad", query, barChart()))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unknown_field")
		require.ErrorContains(t, err, "department")
	})

	t.Run("it rejects an enum value the query endpoint would refuse on replay", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		query := validQuery()
		query["measures"] = []any{map[string]any{"op": "COUNT"}}
		_, err := ti.service.CreateWidget(ctx, createPayload("shouty", query, barChart()))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "measures[0].op")
	})

	t.Run("it rejects an unknown dataset", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		payload := createPayload("bad", validQuery(), barChart())
		payload.Dataset = "departments"
		_, err := ti.service.CreateWidget(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unknown_dataset")
	})

	t.Run("it rejects an absolute window", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		query := validQuery()
		query["window"] = "2026-09-01/2026-09-08"
		_, err := ti.service.CreateWidget(ctx, createPayload("bad", query, barChart()))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "window")
	})

	t.Run("it accepts every dashboard date preset, and the builder's old spelling of a day", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		for _, window := range []string{"15m", "1h", "4h", "1d", "2d", "3d", "7d", "15d", "30d", "90d", "24h"} {
			query := validQuery()
			query["window"] = window
			_, err := ti.service.CreateWidget(ctx, createPayload(window, query, barChart()))
			require.NoError(t, err, "window %s", window)
		}
	})

	t.Run("it rejects a chart that cannot draw the question", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)

		cases := []struct {
			name   string
			query  map[string]any
			chart  widgets.ChartType
			reason string
		}{
			{name: "a timeseries with no grain", query: withQuery(validQuery(), "grain", "none"), chart: widgets.ChartLine, reason: "timeseries"},
			{name: "a timeseries of rows", query: rowsQuery(), chart: widgets.ChartArea, reason: "timeseries"},
			{name: "a number broken down by a dimension", query: withQuery(validQuery(), "grain", "none"), chart: widgets.ChartNumber, reason: "no dimensions"},
			{name: "a ranking with nothing to rank by", query: withQuery(withQuery(validQuery(), "grain", "none"), "dimensions", []any{}), chart: widgets.ChartRanked, reason: "at least one dimension"},
			{name: "a number over time buckets", query: withQuery(validQuery(), "dimensions", []any{}), chart: widgets.ChartNumber, reason: "no grain"},
			{name: "a ranking over time buckets", query: validQuery(), chart: widgets.ChartRanked, reason: "no grain"},
		}
		for _, tc := range cases {
			_, err := ti.service.CreateWidget(ctx, createPayload(tc.name, tc.query, chart(tc.chart)))
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.ErrorContains(t, err, tc.reason, tc.name)
		}
	})

	t.Run("it draws rows only as a table", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.CreateWidget(ctx, createPayload("rows", rowsQuery(), chart(widgets.ChartTable)))
		require.NoError(t, err)

		for _, chartType := range []widgets.ChartType{widgets.ChartLine, widgets.ChartArea, widgets.ChartBar, widgets.ChartNumber, widgets.ChartRanked} {
			_, err := ti.service.CreateWidget(ctx, createPayload("rows as "+string(chartType), rowsQuery(), chart(chartType)))
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.ErrorContains(t, err, "unsatisfiable", chartType)
		}
	})

	t.Run("it ranks by more than one dimension", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		query := withQuery(withQuery(validQuery(), "grain", "none"), "dimensions", []any{"user", "surface"})
		_, err := ti.service.CreateWidget(ctx, createPayload("ranked pairs", query, chart(widgets.ChartRanked)))
		require.NoError(t, err)
	})

	t.Run("it checks and stores a chart type in any case as lowercase", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)

		_, err := ti.service.CreateWidget(ctx, createPayload("ungrained Line", withQuery(validQuery(), "grain", "none"), map[string]any{"type": "Line"}))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "timeseries")

		created, err := ti.service.CreateWidget(ctx, createPayload("Bar", validQuery(), map[string]any{"type": "Bar"}))
		require.NoError(t, err)
		require.Equal(t, string(widgets.ChartBar), created.Visualization["type"])
		got, err := ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, string(widgets.ChartBar), got.Visualization["type"])
	})

	t.Run("it rejects a query key it does not know, naming it", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)

		misspelled := withQuery(validQuery(), "mesures", []any{map[string]any{"op": "count"}})
		_, err := ti.service.CreateWidget(ctx, createPayload("typo", misspelled, barChart()))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "mesures")

		absolute := withQuery(validQuery(), "from", "2026-09-01T00:00:00Z")
		_, err = ti.service.CreateWidget(ctx, createPayload("absolute", absolute, barChart()))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, `"from"`)
	})

	t.Run("it keeps numbers in a request body exact", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)

		// Decoded the way the HTTP server decodes it, since calling the
		// service directly would skip request decoding.
		body := `{"name":"big","dataset":"sessions",` +
			`"query":{"window":"7d","grain":"day","dimensions":["user"],"measures":[{"op":"count"}],"limit":50},` +
			`"visualization":{"type":"bar","options":{"seed":9007199254740993}}}`
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/widgets.create", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		payload, err := widgetssrv.DecodeCreateWidgetRequest(goahttp.NewMuxer(), widgets.RequestDecoder)(req)
		require.NoError(t, err)

		created, err := ti.service.CreateWidget(ctx, payload)
		require.NoError(t, err, "the limit still decodes into the query")
		got, err := ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		encoded, err := json.Marshal(got.Visualization)
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"seed":9007199254740993`)
	})

	t.Run("it rejects a visualization with no type", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.CreateWidget(ctx, createPayload("untyped", validQuery(), map[string]any{"options": map[string]any{}}))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "visualization.type")
	})

	t.Run("it lets any member save", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		memberCtx := asMember(t, ctx, ti, "user_member_"+uuid.NewString())
		created, err := ti.service.CreateWidget(memberCtx, createPayload("member's", validQuery(), barChart()))
		require.NoError(t, err)
		require.NotNil(t, created.CreatedByUserID)
	})

	t.Run("it requires an authenticated project", func(t *testing.T) {
		t.Parallel()
		_, ti := newTestService(t)
		_, err := ti.service.CreateWidget(t.Context(), createPayload("x", validQuery(), barChart()))
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}

func TestListAndGetWidgets(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	first, err := ti.service.CreateWidget(ctx, createPayload("first", validQuery(), barChart()))
	require.NoError(t, err)
	second, err := ti.service.CreateWidget(ctx, createPayload("second", validQuery(), barChart()))
	require.NoError(t, err)

	// A row the catalog no longer accepts, as a catalog change would leave
	// behind. Written directly, since the service refuses it.
	staleRow := insertBrokenWidget(t, ti)

	result, err := ti.service.ListWidgets(ctx, &gen.ListWidgetsPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, result.Widgets, 3)
	require.Equal(t, "stale", result.Widgets[0].Name, "most recently updated first")
	require.NotNil(t, result.Widgets[0].InvalidReason, "a widget a catalog change broke says so on read")
	require.Contains(t, *result.Widgets[0].InvalidReason, "unknown_field")
	require.Equal(t, second.ID, result.Widgets[1].ID)
	require.Nil(t, result.Widgets[1].InvalidReason)
	require.Equal(t, first.ID, result.Widgets[2].ID)

	got, err := ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: first.ID, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, "first", got.Name)
	require.Nil(t, got.InvalidReason)

	gotStale, err := ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: staleRow.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, gotStale.InvalidReason, "get validates on read too")

	// A stored key the query does not know is named on read, not ignored.
	unknownKey := insertStoredWidget(t, ti, "unknown key", []byte(`{"window":"7d","measures":[{"op":"count"}],"from":"2026-09-01T00:00:00Z"}`))
	gotUnknown, err := ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: unknownKey.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, gotUnknown.InvalidReason)
	require.Contains(t, *gotUnknown.InvalidReason, `"from"`)

	_, err = ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: uuid.NewString(), SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.ListWidgets(t.Context(), &gen.ListWidgetsPayload{SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestUpdateWidget(t *testing.T) {
	t.Parallel()

	t.Run("it replaces the widget and audits before and after", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateWidget(ctx, createPayload("before", validQuery(), barChart()))
		require.NoError(t, err)

		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetUpdate)
		require.NoError(t, err)

		updated, err := ti.service.UpdateWidget(ctx, &gen.UpdateWidgetPayload{ID: created.ID, Name: "after", Description: nil, Dataset: "tool_calls",
			Query:         map[string]any{"window": "24h", "dimensions": []any{"tool_name", "status"}, "ungrouped": true},
			Visualization: chart(widgets.ChartTable), SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, "after", updated.Name)
		require.Equal(t, "tool_calls", updated.Dataset)
		require.Equal(t, true, updated.Query["ungrouped"])
		require.Equal(t, string(widgets.ChartTable), updated.Visualization["type"])

		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetUpdate)
		require.NoError(t, err)
		require.Equal(t, before+1, after)

		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWidgetUpdate)
		require.NoError(t, err)
		snapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
		require.NoError(t, err)
		require.Equal(t, "before", snapshot["Name"], "snapshot keys are the view's Go field names")
	})

	t.Run("its creator can update it with membership alone", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		ownerCtx := asMember(t, ctx, ti, "user_owner_"+uuid.NewString())
		created, err := ti.service.CreateWidget(ownerCtx, createPayload("mine", validQuery(), barChart()))
		require.NoError(t, err)

		updated, err := ti.service.UpdateWidget(ownerCtx, &gen.UpdateWidgetPayload{ID: created.ID, Name: "still mine", Description: nil, Dataset: "sessions", Query: validQuery(), Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, "still mine", updated.Name)
	})

	t.Run("another member cannot update it without project write", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateWidget(ctx, createPayload("theirs", validQuery(), barChart()))
		require.NoError(t, err)

		otherCtx := asMember(t, ctx, ti, "user_other_"+uuid.NewString())
		_, err = ti.service.UpdateWidget(otherCtx, &gen.UpdateWidgetPayload{ID: created.ID, Name: "hijacked", Description: nil, Dataset: "sessions", Query: validQuery(), Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeForbidden)

		got, err := ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, "theirs", got.Name, "a refused update changes nothing")

		writerCtx := asMember(t, ctx, ti, "user_writer_"+uuid.NewString(), authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
		updated, err := ti.service.UpdateWidget(writerCtx, &gen.UpdateWidgetPayload{ID: created.ID, Name: "edited", Description: nil, Dataset: "sessions", Query: validQuery(), Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, "edited", updated.Name)
	})

	t.Run("it rejects a chart that cannot draw the new question", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateWidget(ctx, createPayload("before", validQuery(), barChart()))
		require.NoError(t, err)
		_, err = ti.service.UpdateWidget(ctx, &gen.UpdateWidgetPayload{ID: created.ID, Name: "x", Description: nil, Dataset: "sessions",
			Query: rowsQuery(), Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "timeseries")
	})

	t.Run("it reports an unknown or deleted widget as not found", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.UpdateWidget(ctx, &gen.UpdateWidgetPayload{ID: uuid.NewString(), Name: "x", Description: nil, Dataset: "sessions", Query: validQuery(), Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)

		created, err := ti.service.CreateWidget(ctx, createPayload("gone", validQuery(), barChart()))
		require.NoError(t, err)
		require.NoError(t, ti.service.DeleteWidget(ctx, &gen.DeleteWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
		_, err = ti.service.UpdateWidget(ctx, &gen.UpdateWidgetPayload{ID: created.ID, Name: "x", Description: nil, Dataset: "sessions", Query: validQuery(), Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
		err = ti.service.DeleteWidget(ctx, &gen.DeleteWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}

func TestDuplicateWidget(t *testing.T) {
	t.Parallel()

	t.Run("it copies a teammate's widget into one the caller owns", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		description := "Who is calling tools"
		payload := createPayload("Tool calls by user", validQuery(), barChart())
		payload.Description = &description
		source, err := ti.service.CreateWidget(ctx, payload)
		require.NoError(t, err)

		memberID := "user_member_" + uuid.NewString()
		memberCtx := asMember(t, ctx, ti, memberID)
		copied, err := ti.service.DuplicateWidget(memberCtx, &gen.DuplicateWidgetPayload{ID: source.ID, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.NotEqual(t, source.ID, copied.ID)
		require.Equal(t, "Tool calls by user (copy)", copied.Name)
		require.Equal(t, &description, copied.Description)
		require.Equal(t, source.Dataset, copied.Dataset)
		require.Equal(t, source.Query, copied.Query)
		require.Equal(t, source.Visualization, copied.Visualization)
		require.NotNil(t, copied.CreatedByUserID)
		require.Equal(t, memberID, *copied.CreatedByUserID, "the copy belongs to whoever duplicated it")

		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWidgetCreate)
		require.NoError(t, err)
		metadata, err := audittest.DecodeAuditData(record.Metadata)
		require.NoError(t, err)
		require.Equal(t, "widget:"+source.ID, metadata["duplicated_from"])

		// The copy is its own widget: the member can delete it with
		// membership alone, and the original stays.
		require.NoError(t, ti.service.DeleteWidget(memberCtx, &gen.DeleteWidgetPayload{ID: copied.ID, SessionToken: nil, ProjectSlugInput: nil}))
		_, err = ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: source.ID, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
	})

	t.Run("it keeps a long name within the limit", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		long := strings.Repeat("é", 200)
		source, err := ti.service.CreateWidget(ctx, createPayload(long, validQuery(), barChart()))
		require.NoError(t, err)
		copied, err := ti.service.DuplicateWidget(ctx, &gen.DuplicateWidgetPayload{ID: source.ID, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(copied.Name, " (copy)"))
		require.Len(t, []rune(copied.Name), 200)
	})

	t.Run("it refuses a widget the catalog has broken, naming the field", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		row := insertBrokenWidget(t, ti)

		_, err := ti.service.DuplicateWidget(ctx, &gen.DuplicateWidgetPayload{ID: row.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "department")
	})

	t.Run("it reports an unknown widget", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.DuplicateWidget(ctx, &gen.DuplicateWidgetPayload{ID: uuid.NewString(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}

func TestDeleteWidget(t *testing.T) {
	t.Parallel()

	t.Run("its creator can delete it with membership alone", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		memberCtx := asMember(t, ctx, ti, "user_owner_"+uuid.NewString())
		created, err := ti.service.CreateWidget(memberCtx, createPayload("mine", validQuery(), barChart()))
		require.NoError(t, err)

		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetDelete)
		require.NoError(t, err)
		require.NoError(t, ti.service.DeleteWidget(memberCtx, &gen.DeleteWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetDelete)
		require.NoError(t, err)
		require.Equal(t, before+1, after)

		listed, err := ti.service.ListWidgets(ctx, &gen.ListWidgetsPayload{SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		for _, w := range listed.Widgets {
			require.NotEqual(t, created.ID, w.ID, "deleted widgets are gone from the list")
		}
	})

	t.Run("another member cannot delete it without project write", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateWidget(ctx, createPayload("theirs", validQuery(), barChart()))
		require.NoError(t, err)

		otherCtx := asMember(t, ctx, ti, "user_other_"+uuid.NewString())
		err = ti.service.DeleteWidget(otherCtx, &gen.DeleteWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeForbidden)

		writerCtx := asMember(t, ctx, ti, "user_writer_"+uuid.NewString(), authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
		require.NoError(t, ti.service.DeleteWidget(writerCtx, &gen.DeleteWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
	})
}

// insertBrokenWidget stores a widget the catalog no longer accepts, as a
// catalog change would leave behind: it asks for a dimension, "department",
// the sessions dataset does not have.
func insertBrokenWidget(t *testing.T, ti *testInstance) widgetsrepo.Widget {
	t.Helper()
	stale, err := json.Marshal(map[string]any{"window": "7d", "dimensions": []string{"department"}, "measures": []map[string]any{{"op": "count"}}})
	require.NoError(t, err)
	return insertStoredWidget(t, ti, "stale", stale)
}

// insertStoredWidget writes a sessions table widget straight to the
// database, since the service refuses a query it cannot plan.
func insertStoredWidget(t *testing.T, ti *testInstance, name string, query []byte) widgetsrepo.Widget {
	t.Helper()
	row, err := widgetsrepo.New(ti.conn).CreateWidget(t.Context(), widgetsrepo.CreateWidgetParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
		Name: name, Description: pgtype.Text{String: "", Valid: false}, Dataset: "sessions", Query: query, Visualization: []byte(`{"type":"table"}`),
	})
	require.NoError(t, err)
	return row
}

// withQuery returns query with one key replaced.
func withQuery(query map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(query))
	maps.Copy(out, query)
	out[key] = value
	return out
}

// rowsQuery asks for rows at the dataset's grain: nothing measured.
func rowsQuery() map[string]any {
	return map[string]any{"window": "24h", "grain": "none", "dimensions": []any{"user"}, "ungrouped": true}
}

func TestWidgetDashboards(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateWidget(ctx, createPayload("placed", validQuery(), barChart()))
	require.NoError(t, err)
	require.Empty(t, created.Dashboards, "a new widget is on no dashboard")
	widgetID := uuid.MustParse(created.ID)

	// Two dashboards, written directly: this is about what the widget says.
	dashboards := dashboardsrepo.New(ti.conn)
	newDashboard := func(name string) dashboardsrepo.Dashboard {
		row, err := dashboards.CreateDashboard(ctx, dashboardsrepo.CreateDashboardParams{
			ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
			Name: name, Description: pgtype.Text{String: "", Valid: false}, Filters: []byte("{}"),
		})
		require.NoError(t, err)
		return row
	}
	place := func(dashboard dashboardsrepo.Dashboard, x int32) {
		_, err := dashboards.InsertPlacement(ctx, dashboardsrepo.InsertPlacementParams{
			ProjectID: ti.projectID, OrganizationID: ti.orgID, DashboardID: dashboard.ID, WidgetID: widgetID, X: x, Y: 0, W: 4, H: 3,
		})
		require.NoError(t, err)
	}
	alpha, beta := newDashboard("Alpha"), newDashboard("Beta")
	place(alpha, 0)
	place(alpha, 4)
	place(beta, 0)

	got, err := ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []*gen.WidgetDashboard{{ID: alpha.ID.String(), Name: "Alpha"}, {ID: beta.ID.String(), Name: "Beta"}}, got.Dashboards, "each dashboard once, however many cards show the widget")

	listed, err := ti.service.ListWidgets(ctx, &gen.ListWidgetsPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Widgets, 1)
	require.Equal(t, got.Dashboards, listed.Widgets[0].Dashboards)

	updated, err := ti.service.UpdateWidget(ctx, &gen.UpdateWidgetPayload{ID: created.ID, Name: "renamed", Description: nil, Dataset: "sessions", Query: validQuery(), Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, got.Dashboards, updated.Dashboards, "an edit says where it reached")

	// A deleted dashboard no longer counts.
	_, err = dashboards.DeleteDashboard(ctx, dashboardsrepo.DeleteDashboardParams{ProjectID: ti.projectID, ID: beta.ID})
	require.NoError(t, err)
	got, err = ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []*gen.WidgetDashboard{{ID: alpha.ID.String(), Name: "Alpha"}}, got.Dashboards)

	// Deleting the widget takes it off the dashboard; the dashboard stays.
	// Read through the widgets repo's dashboards query, which joins dashboards
	// only and so does not hide a deleted widget's cards: rows left behind
	// would show.
	require.NoError(t, ti.service.DeleteWidget(ctx, &gen.DeleteWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
	left, err := widgetsrepo.New(ti.conn).ListDashboardsForWidget(ctx, widgetsrepo.ListDashboardsForWidgetParams{ProjectID: ti.projectID, WidgetID: uuid.MustParse(created.ID)})
	require.NoError(t, err)
	require.Empty(t, left)

	// The dashboard it came off is touched and its history says why the
	// cards went; the widget's own event names the dashboard.
	touched, err := dashboards.GetDashboard(ctx, dashboardsrepo.GetDashboardParams{ProjectID: ti.projectID, ID: alpha.ID})
	require.NoError(t, err)
	require.True(t, touched.UpdatedAt.Time.After(alpha.UpdatedAt.Time), "the dashboard moves up the list")
	layout, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDashboardLayout)
	require.NoError(t, err)
	require.Equal(t, alpha.ID.String(), layout.SubjectID)
	require.NotEmpty(t, layout.BeforeSnapshot)
	require.JSONEq(t, "[]", string(layout.AfterSnapshot))
	deleted, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWidgetDelete)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(deleted.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{"dashboard:" + alpha.ID.String()}, metadata["removed_from"])
	_, err = dashboards.GetDashboard(ctx, dashboardsrepo.GetDashboardParams{ProjectID: ti.projectID, ID: alpha.ID})
	require.NoError(t, err)
}

func TestDeleteWidgetWaitsForALayoutHoldingTheWidget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateWidget(ctx, createPayload("held", validQuery(), barChart()))
	require.NoError(t, err)
	widgetID := uuid.MustParse(created.ID)
	dashboards := dashboardsrepo.New(ti.conn)
	dashboard, err := dashboards.CreateDashboard(ctx, dashboardsrepo.CreateDashboardParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
		Name: "Held", Description: pgtype.Text{String: "", Valid: false}, Filters: []byte("{}"),
	})
	require.NoError(t, err)

	// A layout save reads the widget for share and places a card on it,
	// and holds both until it commits.
	holding, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: a transaction held open to pin down the lock a layout save takes
	require.NoError(t, err)
	t.Cleanup(func() { _ = holding.Rollback(ctx) })
	_, err = dashboardsrepo.New(holding).GetWidgetForPlacement(ctx, dashboardsrepo.GetWidgetForPlacementParams{ProjectID: ti.projectID, ID: widgetID})
	require.NoError(t, err)
	_, err = dashboardsrepo.New(holding).InsertPlacement(ctx, dashboardsrepo.InsertPlacementParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, DashboardID: dashboard.ID, WidgetID: widgetID, X: 0, Y: 0, W: 4, H: 3,
	})
	require.NoError(t, err)

	// Deleting the widget meanwhile waits for the layout, so the card it
	// placed is taken off with the rest rather than left behind.
	done := make(chan error, 1)
	go func() {
		done <- ti.service.DeleteWidget(ctx, &gen.DeleteWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
	}()
	select {
	case err := <-done:
		t.Fatalf("the delete did not wait for the layout: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, holding.Commit(ctx))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the delete did not finish once the layout had committed")
	}
	left, err := widgetsrepo.New(ti.conn).ListDashboardsForWidget(ctx, widgetsrepo.ListDashboardsForWidgetParams{ProjectID: ti.projectID, WidgetID: widgetID})
	require.NoError(t, err)
	require.Empty(t, left)
}
