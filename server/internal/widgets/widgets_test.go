package widgets_test

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	return map[string]any{"type": "bar", "options": map[string]any{}}
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
		require.Equal(t, "bar", created.Visualization["type"])
		require.Equal(t, "day", created.Query["grain"])
		require.Nil(t, created.InvalidReason)

		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetCreate)
		require.NoError(t, err)
		require.Equal(t, before+1, after)
	})

	t.Run("it keeps visualization options it does not interpret", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		chart := map[string]any{"type": "bar", "options": map[string]any{"stack": "normal", "series_limit": 12}}
		created, err := ti.service.CreateWidget(ctx, createPayload("stacked", validQuery(), chart))
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

	t.Run("it rejects a chart that cannot draw the question", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)

		cases := []struct {
			name   string
			query  map[string]any
			chart  string
			reason string
		}{
			{name: "a timeseries with no grain", query: withQuery(validQuery(), "grain", "none"), chart: "line", reason: "timeseries"},
			{name: "a timeseries of rows", query: rowsQuery(), chart: "area", reason: "timeseries"},
			{name: "a number broken down by a dimension", query: withQuery(validQuery(), "grain", "none"), chart: "number", reason: "no dimensions"},
			{name: "a ranking with nothing to rank by", query: withQuery(withQuery(validQuery(), "grain", "none"), "dimensions", []any{}), chart: "ranked", reason: "one dimension"},
			{name: "a number over time buckets", query: withQuery(validQuery(), "dimensions", []any{}), chart: "number", reason: "no grain"},
			{name: "a ranking over time buckets", query: validQuery(), chart: "ranked", reason: "no grain"},
		}
		for _, tc := range cases {
			_, err := ti.service.CreateWidget(ctx, createPayload(tc.name, tc.query, map[string]any{"type": tc.chart}))
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.ErrorContains(t, err, tc.reason, tc.name)
		}
	})

	t.Run("it draws rows only as a table", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.CreateWidget(ctx, createPayload("rows", rowsQuery(), map[string]any{"type": "table"}))
		require.NoError(t, err)
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
	stale, err := json.Marshal(map[string]any{"window": "7d", "dimensions": []string{"department"}, "measures": []map[string]any{{"op": "count"}}})
	require.NoError(t, err)
	staleRow, err := widgetsrepo.New(ti.conn).CreateWidget(ctx, widgetsrepo.CreateWidgetParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
		Name: "stale", Description: pgtype.Text{String: "", Valid: false}, Dataset: "sessions", Query: stale, Visualization: []byte(`{"type":"table"}`),
	})
	require.NoError(t, err)

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
			Visualization: map[string]any{"type": "table"}, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, "after", updated.Name)
		require.Equal(t, "tool_calls", updated.Dataset)
		require.Equal(t, true, updated.Query["ungrouped"])
		require.Equal(t, "table", updated.Visualization["type"])

		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWidgetUpdate)
		require.NoError(t, err)
		require.Equal(t, before+1, after)

		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWidgetUpdate)
		require.NoError(t, err)
		snapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
		require.NoError(t, err)
		require.Equal(t, "before", snapshot["Name"], "snapshot keys are the view's Go field names")
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
		stale, err := json.Marshal(map[string]any{"window": "7d", "dimensions": []string{"department"}, "measures": []map[string]any{{"op": "count"}}})
		require.NoError(t, err)
		row, err := widgetsrepo.New(ti.conn).CreateWidget(ctx, widgetsrepo.CreateWidgetParams{
			ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
			Name: "stale", Description: pgtype.Text{String: "", Valid: false}, Dataset: "sessions", Query: stale, Visualization: []byte(`{"type":"table"}`),
		})
		require.NoError(t, err)

		_, err = ti.service.DuplicateWidget(ctx, &gen.DuplicateWidgetPayload{ID: row.ID.String(), SessionToken: nil, ProjectSlugInput: nil})
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
