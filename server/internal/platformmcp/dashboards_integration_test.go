package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dashboards"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	widgetsrepo "github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

type dashboardToolFixture struct {
	reg       *Registrar
	service   *DashboardsService
	db        *pgxpool.Pool
	flags     *feature.InMemory
	principal Principal
	project   ResolvedProject
}

// newDashboardToolFixture registers the live dashboard tools against a
// cloned database, with Explore on and a caller holding project:read and
// project:write on the project.
func newDashboardToolFixture(t *testing.T) (context.Context, dashboardToolFixture) {
	t.Helper()

	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_dashboards")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	principal.ClientID = "dashboard-tools-test-client"
	principal.Surface = SurfacePlatformMCP
	logger, tracer := testenv.NewLogger(t), testenv.NewTracerProvider(t)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagExplore, principal.OrganizationID, true)
	engine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	management := dashboards.NewService(logger, tracer, db, &sessions.Manager{}, engine, audit.NewLogger())
	service := NewDashboardsService(logger, management, NewPostgresReader(logger, db), engine, flags, NewPostgresOrganizationSlugResolver(db), "test-dashboard-key")
	require.NotNil(t, service)
	require.Nil(t, NewDashboardsService(logger, management, NewPostgresReader(logger, db), engine, nil, NewPostgresOrganizationSlugResolver(db), "test-dashboard-key"), "without flags the tools are served as unavailable")
	reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
	registerDashboardTools(reg, service)

	ctx := dashboardCallerContext(t.Context(), principal, project, authz.ScopeProjectRead, authz.ScopeProjectWrite)
	return ctx, dashboardToolFixture{reg: reg, service: service, db: db, flags: flags, principal: principal, project: project}
}

func dashboardCallerContext(ctx context.Context, principal Principal, project ResolvedProject, scopes ...authz.Scope) context.Context {
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextWithPrincipal(ctx, principal)
	grants := make([]authz.Grant, 0, len(scopes))
	for _, scope := range scopes {
		grants = append(grants, authz.NewGrant(scope, project.ID.String()))
	}
	return authz.GrantsToContext(ctx, grants)
}

func (f dashboardToolFixture) call(t *testing.T, ctx context.Context, tool string, arguments map[string]any) (any, error) {
	t.Helper()
	arguments["project_id"] = f.project.ID.String()
	raw, err := json.Marshal(arguments)
	require.NoError(t, err)
	return descriptorByName(t, f.reg, tool).Invoke(ctx, raw)
}

func (f dashboardToolFixture) write(t *testing.T, ctx context.Context, tool string, arguments map[string]any) dashboardWriteOutput {
	t.Helper()
	value, err := f.call(t, ctx, tool, arguments)
	require.NoError(t, err)
	out, ok := value.(dashboardWriteOutput)
	require.True(t, ok)
	return out
}

// previewAndConfirm previews a write, then confirms it with the preview's
// version and token under the given key.
func (f dashboardToolFixture) previewAndConfirm(t *testing.T, ctx context.Context, tool string, arguments map[string]any, key string) dashboardWriteOutput {
	t.Helper()
	arguments["confirmed"] = false
	preview := f.write(t, ctx, tool, arguments)
	require.True(t, preview.Preview)
	require.NotEmpty(t, preview.PreviewToken)
	return f.write(t, ctx, tool, confirmed(arguments, preview, key))
}

func (o dashboardWriteOutput) previewed() (version, token string) { return o.Version, o.PreviewToken }

func (f dashboardToolFixture) insertWidget(t *testing.T, name, visualization string) widgetsrepo.Widget {
	t.Helper()
	row, err := widgetsrepo.New(f.db).CreateWidget(t.Context(), widgetsrepo.CreateWidgetParams{
		ProjectID: f.project.ID, OrganizationID: f.principal.OrganizationID, CreatedByUserID: pgtype.Text{String: f.principal.UserID, Valid: true},
		Name: name, Description: pgtype.Text{String: "", Valid: false}, Dataset: "tool_calls",
		Query: []byte(`{"window":"7d","grain":"day","dimensions":["user"],"measures":[{"op":"count","alias":"count"}],"limit":1000}`), Visualization: []byte(visualization),
	})
	require.NoError(t, err)
	return row
}

func refusalCode(t *testing.T, err error) string {
	t.Helper()
	refusal, ok := errors.AsType[*ToolRefusalError](err)
	require.True(t, ok, "expected a refusal, got %v", err)
	var payload struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &payload), refusal.Payload)
	return payload.Code
}

func TestDashboardToolsCreateLayOutAndRead(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)

	created := f.previewAndConfirm(t, ctx, saveDashboardToolName, map[string]any{"name": "Agent usage"}, "create-1")
	require.False(t, created.Preview)
	require.Equal(t, dashboardSnapshotCurrent, created.SnapshotScope)
	require.NotNil(t, created.Current)
	require.Equal(t, "Agent usage", created.Current.Name)
	dashboardID := created.DashboardID

	got, err := f.call(t, ctx, getDashboardToolName, map[string]any{"dashboard_id": dashboardID})
	require.NoError(t, err)
	view, ok := got.(dashboardView)
	require.True(t, ok)
	require.Equal(t, created.Current.Version, view.Version)
	require.Empty(t, view.Cards)

	widget := f.insertWidget(t, "Tool calls by user", `{"type":"bar","options":{}}`)
	preview := f.write(t, ctx, editDashboardLayoutToolName, map[string]any{
		"dashboard_id": dashboardID,
		"operations":   []any{map[string]any{"op": "add_card", "widget_id": widget.ID.String()}},
		"confirmed":    false,
	})
	require.Len(t, preview.Proposed.Cards, 1)
	require.Equal(t, "Tool calls by user", preview.Proposed.Cards[0].WidgetName, "the preview names the widget a new card shows")
	require.Equal(t, widget.ID.String(), preview.Proposed.Cards[0].WidgetID)
	require.Empty(t, preview.Proposed.Cards[0].PlacementID, "a card the preview places has no id until it is confirmed")
	require.Empty(t, preview.Current.Cards)
	added := f.previewAndConfirm(t, ctx, editDashboardLayoutToolName, map[string]any{
		"dashboard_id": dashboardID,
		"operations":   []any{map[string]any{"op": "add_card", "widget_id": widget.ID.String()}},
	}, "layout-1")
	require.Len(t, added.Current.Cards, 1)
	card := added.Current.Cards[0]
	require.Equal(t, dashboardCard{PlacementID: card.PlacementID, WidgetID: widget.ID.String(), WidgetName: "Tool calls by user", Dataset: "tool_calls", ChartType: "bar", X: 0, Y: 0, W: 6, H: 3}, card)

	movePreview := f.write(t, ctx, editDashboardLayoutToolName, map[string]any{
		"dashboard_id": dashboardID,
		"operations":   []any{map[string]any{"op": "move_card", "placement_id": card.PlacementID, "x": 4, "y": 2, "w": 8, "h": 4}},
		"confirmed":    false,
	})
	require.Equal(t, card.PlacementID, movePreview.Proposed.Cards[0].PlacementID, "a moved card keeps its id")
	moved := f.previewAndConfirm(t, ctx, editDashboardLayoutToolName, map[string]any{
		"dashboard_id": dashboardID,
		"operations":   []any{map[string]any{"op": "move_card", "placement_id": card.PlacementID, "x": 4, "y": 2, "w": 8, "h": 4}},
	}, "layout-2")
	require.Equal(t, dashboardCard{PlacementID: card.PlacementID, WidgetID: widget.ID.String(), WidgetName: "Tool calls by user", Dataset: "tool_calls", ChartType: "bar", X: 4, Y: 2, W: 8, H: 4}, moved.Current.Cards[0])

	listed, err := f.call(t, ctx, listDashboardsToolName, map[string]any{})
	require.NoError(t, err)
	list, ok := listed.(listDashboardsOutput)
	require.True(t, ok)
	var builtIn, mine bool
	for _, summary := range list.Dashboards {
		builtIn = builtIn || (summary.BuiltIn && summary.Slug == "mcp-tools")
		if summary.ID == dashboardID {
			mine = true
			require.Equal(t, 1, summary.CardCount)
			require.Equal(t, f.principal.UserID, *summary.CreatedByUserID)
		}
	}
	require.True(t, builtIn, "the Speakeasy-built dashboards are listed")
	require.True(t, mine)
}

func TestDashboardToolsDuplicateABuiltInBySlug(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)

	builtIn, err := f.call(t, ctx, getDashboardToolName, map[string]any{"dashboard_id": "mcp-tools"})
	require.NoError(t, err)
	page, ok := builtIn.(dashboardView)
	require.True(t, ok)
	require.True(t, page.BuiltIn)
	require.NotEmpty(t, page.Cards)

	widgetsBefore, err := widgetsrepo.New(f.db).ListWidgets(ctx, widgetsrepo.ListWidgetsParams{ProjectID: f.project.ID, RowLimit: pgtype.Int4{Int32: 0, Valid: false}})
	require.NoError(t, err)
	preview := f.write(t, ctx, duplicateDashboardToolName, map[string]any{"dashboard_id": "mcp-tools", "confirmed": false})
	require.Len(t, preview.Proposed.Cards, len(page.Cards))
	for _, card := range preview.Proposed.Cards {
		require.Empty(t, card.WidgetID, "the copy's widgets do not exist until it is confirmed")
		require.NotEmpty(t, card.WidgetName)
	}
	copied := f.previewAndConfirm(t, ctx, duplicateDashboardToolName, map[string]any{"dashboard_id": "mcp-tools"}, "copy-1")
	require.False(t, copied.Current.BuiltIn)
	require.Equal(t, "MCP & Tools (copy)", copied.Current.Name)
	require.Len(t, copied.Current.Cards, len(page.Cards))
	widgetsAfter, err := widgetsrepo.New(f.db).ListWidgets(ctx, widgetsrepo.ListWidgetsParams{ProjectID: f.project.ID, RowLimit: pgtype.Int4{Int32: 0, Valid: false}})
	require.NoError(t, err)
	require.Len(t, widgetsAfter, len(widgetsBefore)+len(page.Cards), "each card of the copy is a new saved widget")
	for _, card := range copied.Current.Cards {
		require.NotEmpty(t, card.WidgetID)
		require.NotEmpty(t, card.WidgetName)
	}
}

func (f dashboardToolFixture) get(t *testing.T, ctx context.Context, dashboardID string) dashboardView {
	t.Helper()
	value, err := f.call(t, ctx, getDashboardToolName, map[string]any{"dashboard_id": dashboardID})
	require.NoError(t, err)
	view, ok := value.(dashboardView)
	require.True(t, ok)
	return view
}

func TestDashboardToolsReplayAndConflicts(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)

	created := f.previewAndConfirm(t, ctx, saveDashboardToolName, map[string]any{"name": "Usage"}, "create-1")
	dashboardID := created.DashboardID

	preview := f.write(t, ctx, saveDashboardToolName, map[string]any{"dashboard_id": dashboardID, "name": "Renamed", "confirmed": false})
	require.Equal(t, created.Version, preview.Version)
	require.Equal(t, "Usage", preview.Current.Name)
	require.Equal(t, "Renamed", preview.Proposed.Name)
	require.Equal(t, "Usage", f.get(t, ctx, dashboardID).Name, "a preview saves nothing")

	confirm := func() map[string]any {
		return map[string]any{"dashboard_id": dashboardID, "name": "Renamed", "confirmed": true, "expected_version": preview.Version, "preview_token": preview.PreviewToken, "idempotency_key": "rename-1"}
	}
	for _, missing := range []string{"idempotency_key", "expected_version", "preview_token"} {
		arguments := confirm()
		delete(arguments, missing)
		_, err := f.call(t, ctx, saveDashboardToolName, arguments)
		require.Equal(t, "invalid_request", refusalCode(t, err), missing)
	}
	otherProposal := confirm()
	otherProposal["name"] = "Something else"
	_, err := f.call(t, ctx, saveDashboardToolName, otherProposal)
	require.Equal(t, "conflict", refusalCode(t, err), "a token for another proposal")

	before, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionDashboardUpdate)
	require.NoError(t, err)
	first := f.write(t, ctx, saveDashboardToolName, confirm())
	require.False(t, first.Replayed)
	require.Equal(t, "Renamed", first.Current.Name)
	replayed := f.write(t, ctx, saveDashboardToolName, confirm())
	require.True(t, replayed.Replayed)
	require.Equal(t, first.ReceiptID, replayed.ReceiptID)
	after, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionDashboardUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "the replay changes nothing")

	// A preview taken now, then made stale by another write before it is
	// confirmed, is refused as a conflict under the dashboard's row lock.
	stalePreview := f.write(t, ctx, saveDashboardToolName, map[string]any{"dashboard_id": dashboardID, "name": "Stale", "confirmed": false})
	require.NotEqual(t, preview.Version, stalePreview.Version)
	f.previewAndConfirm(t, ctx, saveDashboardFiltersToolName, map[string]any{"dashboard_id": dashboardID, "filters": map[string]any{"range": map[string]any{"preset": "30d"}}}, "filters-1")
	_, err = f.call(t, ctx, saveDashboardToolName, map[string]any{"dashboard_id": dashboardID, "name": "Stale", "confirmed": true, "expected_version": stalePreview.Version, "preview_token": stalePreview.PreviewToken, "idempotency_key": "stale-1"})
	require.Equal(t, "conflict", refusalCode(t, err))
	current := f.get(t, ctx, dashboardID)
	require.Equal(t, "Renamed", current.Name)
	require.Equal(t, "30d", *current.Filters.Range.Preset)

	deleted := f.previewAndConfirm(t, ctx, deleteDashboardToolName, map[string]any{"dashboard_id": dashboardID}, "delete-1")
	require.True(t, deleted.Deleted)
	require.Equal(t, dashboardSnapshotDeleted, deleted.SnapshotScope)
	_, err = f.call(t, ctx, getDashboardToolName, map[string]any{"dashboard_id": dashboardID})
	require.Equal(t, "not_found", refusalCode(t, err))
}

func TestDashboardToolsReplayAfterTheDashboardIsDeleted(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)
	created := f.previewAndConfirm(t, ctx, saveDashboardToolName, map[string]any{"name": "Usage"}, "create-1")
	dashboardID := created.DashboardID

	rename := map[string]any{"dashboard_id": dashboardID, "name": "Renamed", "confirmed": false}
	preview := f.write(t, ctx, saveDashboardToolName, rename)
	confirm := map[string]any{"dashboard_id": dashboardID, "name": "Renamed", "confirmed": true, "expected_version": preview.Version, "preview_token": preview.PreviewToken, "idempotency_key": "rename-1"}
	first := f.write(t, ctx, saveDashboardToolName, confirm)
	require.Equal(t, dashboardSnapshotCurrent, first.SnapshotScope)

	f.previewAndConfirm(t, ctx, deleteDashboardToolName, map[string]any{"dashboard_id": dashboardID}, "delete-1")

	replayed := f.write(t, ctx, saveDashboardToolName, confirm)
	require.True(t, replayed.Replayed)
	require.Equal(t, first.ReceiptID, replayed.ReceiptID)
	require.Equal(t, dashboardSnapshotDeleted, replayed.SnapshotScope)
	require.Nil(t, replayed.Current)
}

// unreadableDashboards fails every read, as a post-commit verification read can.
type unreadableDashboards struct{ dashboardsManagement }

func (unreadableDashboards) ReadDashboard(context.Context, string) (dashboards.Detail, error) {
	return dashboards.Detail{}, errors.New("read replica unavailable")
}

func TestDashboardToolsReportAnUnverifiedCommit(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)
	management := f.service.management
	f.service.management = unreadableDashboards{dashboardsManagement: management}

	arguments := map[string]any{"name": "Committed blind", "confirmed": false}
	confirm := confirmed(arguments, f.write(t, ctx, saveDashboardToolName, arguments), "blind")
	committed := f.write(t, ctx, saveDashboardToolName, confirm)
	require.False(t, committed.Replayed)
	require.Equal(t, dashboardSnapshotVerificationUnavailable, committed.SnapshotScope)
	require.NotEmpty(t, committed.ReceiptID)
	require.Nil(t, committed.Current)

	f.service.management = management
	require.Equal(t, "Committed blind", f.get(t, ctx, committed.DashboardID).Name, "the write committed")
	replayed := f.write(t, ctx, saveDashboardToolName, confirm)
	require.True(t, replayed.Replayed)
	require.Equal(t, dashboardSnapshotCurrent, replayed.SnapshotScope)
}

func TestADashboardPreviewCommitsUnderOneKey(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)
	source := f.previewAndConfirm(t, ctx, saveDashboardToolName, map[string]any{"name": "Source"}, "source")

	for _, write := range []struct {
		name      string
		tool      string
		arguments map[string]any
	}{
		{"create", saveDashboardToolName, map[string]any{"name": "Once", "confirmed": false}},
		{"duplicate", duplicateDashboardToolName, map[string]any{"dashboard_id": source.DashboardID, "confirmed": false}},
		{"duplicate built-in", duplicateDashboardToolName, map[string]any{"dashboard_id": "mcp-tools", "confirmed": false}},
	} {
		preview := f.write(t, ctx, write.tool, write.arguments)
		first := f.write(t, ctx, write.tool, confirmed(write.arguments, preview, write.name+"-first"))

		_, err := f.call(t, ctx, write.tool, confirmed(write.arguments, preview, write.name+"-second"))
		require.Equal(t, "preview_used", refusalCode(t, err), write.name)

		replayed := f.write(t, ctx, write.tool, confirmed(write.arguments, preview, write.name+"-first"))
		require.True(t, replayed.Replayed, write.name)
		require.Equal(t, first.DashboardID, replayed.DashboardID, write.name)

		again := f.write(t, ctx, write.tool, write.arguments)
		require.NotEqual(t, preview.PreviewToken, again.PreviewToken, "every preview is distinct")
		f.write(t, ctx, write.tool, confirmed(write.arguments, again, write.name+"-again"))
	}

	listed, err := f.call(t, ctx, listDashboardsToolName, map[string]any{})
	require.NoError(t, err)
	list, ok := listed.(listDashboardsOutput)
	require.True(t, ok)
	var mine int
	for _, summary := range list.Dashboards {
		if !summary.BuiltIn {
			mine++
		}
	}
	require.Equal(t, 7, mine, "the source, plus two creates and two copies of each source")
}

func TestADashboardPreviewExpiresWithItsReceipt(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)
	arguments := map[string]any{"name": "Late", "confirmed": false}
	preview := f.write(t, ctx, saveDashboardToolName, arguments)

	f.service.now = func() time.Time { return time.Now().Add(receiptLifetime + time.Minute) }
	_, err := f.call(t, ctx, saveDashboardToolName, confirmed(arguments, preview, "late"))
	require.Equal(t, "preview_expired", refusalCode(t, err))
}

func TestDashboardToolsRefuseBuiltInsAndBrokenLayouts(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)
	created := f.previewAndConfirm(t, ctx, saveDashboardToolName, map[string]any{"name": "Usage"}, "create-1")
	widget := f.insertWidget(t, "Tool calls by user", `{"type":"bar","options":{}}`)
	added := f.previewAndConfirm(t, ctx, editDashboardLayoutToolName, map[string]any{
		"dashboard_id": created.DashboardID,
		"operations":   []any{map[string]any{"op": "add_card", "widget_id": widget.ID.String()}},
	}, "layout-1")
	placementID := added.Current.Cards[0].PlacementID

	for tool, arguments := range map[string]map[string]any{
		saveDashboardToolName:        {"dashboard_id": "mcp-tools", "name": "Mine"},
		deleteDashboardToolName:      {"dashboard_id": "mcp-tools"},
		editDashboardLayoutToolName:  {"dashboard_id": "mcp-tools", "operations": []any{map[string]any{"op": "remove_card", "placement_id": uuid.NewString()}}},
		saveDashboardFiltersToolName: {"dashboard_id": "mcp-tools", "filters": map[string]any{}},
	} {
		arguments["confirmed"] = false
		_, err := f.call(t, ctx, tool, arguments)
		require.Equal(t, "built_in_read_only", refusalCode(t, err), tool)
	}

	for name, operation := range map[string]map[string]any{
		"overlap":        {"op": "add_card", "widget_id": widget.ID.String(), "x": 2, "y": 0},
		"minimum size":   {"op": "move_card", "placement_id": placementID, "x": 0, "y": 0, "w": 2, "h": 2},
		"unknown widget": {"op": "add_card", "widget_id": uuid.NewString()},
	} {
		_, err := f.call(t, ctx, editDashboardLayoutToolName, map[string]any{"dashboard_id": created.DashboardID, "operations": []any{operation}, "confirmed": false})
		require.Equal(t, "invalid_request", refusalCode(t, err), name)
	}
	require.Len(t, f.get(t, ctx, created.DashboardID).Cards, 1, "a refused preview changes nothing")
}

func TestDashboardToolsCheckProjectAccessFirst(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)
	created := f.previewAndConfirm(t, ctx, saveDashboardToolName, map[string]any{"name": "Usage"}, "create-1")

	// The creator needs only project:read.
	creator := dashboardCallerContext(t.Context(), f.principal, f.project, authz.ScopeProjectRead)
	renamed := f.previewAndConfirm(t, creator, saveDashboardToolName, map[string]any{"dashboard_id": created.DashboardID, "name": "Mine"}, "rename-1")
	require.Equal(t, "Mine", renamed.Current.Name)

	// Anyone else needs project:write, which the dashboards service decides.
	other := f.principal
	other.UserID = "user_" + uuid.NewString()
	reader := dashboardCallerContext(t.Context(), other, f.project, authz.ScopeProjectRead)
	require.Equal(t, "Mine", f.get(t, reader, created.DashboardID).Name, "a member reads")
	_, err := f.call(t, reader, saveDashboardToolName, map[string]any{"dashboard_id": created.DashboardID, "name": "Theirs", "confirmed": false})
	refusal, ok := errors.AsType[*ToolRefusalError](err)
	require.True(t, ok, "%v", err)
	require.JSONEq(t, `{"code":"owner_or_write_required","feature":"dashboards","message":"Only the dashboard's creator, or someone with project:write, may change it."}`, refusal.Payload, "a non-creator with project:read is refused by the owner rule")
	_, err = f.call(t, reader, deleteDashboardToolName, map[string]any{"dashboard_id": created.DashboardID, "confirmed": false})
	require.Equal(t, widgetOwnerOrWriteRequired, refusalCode(t, err))

	writer := dashboardCallerContext(t.Context(), other, f.project, authz.ScopeProjectRead, authz.ScopeProjectWrite)
	theirs := f.previewAndConfirm(t, writer, saveDashboardToolName, map[string]any{"dashboard_id": created.DashboardID, "name": "Theirs"}, "rename-2")
	require.Equal(t, "Theirs", theirs.Current.Name, "project:write changes anyone's dashboard")

	nobody := dashboardCallerContext(t.Context(), other, f.project)
	_, err = f.call(t, nobody, getDashboardToolName, map[string]any{"dashboard_id": uuid.NewString()})
	require.Equal(t, "permission_denied", refusalCode(t, err), "without project:read a dashboard reads as denied, never as not found")
}

func TestDashboardToolsNeedExplore(t *testing.T) {
	t.Parallel()
	ctx, f := newDashboardToolFixture(t)
	f.flags.SetFlag(feature.FlagExplore, f.principal.OrganizationID, false)

	_, err := f.call(t, ctx, listDashboardsToolName, map[string]any{})
	require.Equal(t, analyticsNotEnabledCode, refusalCode(t, err))
	_, err = f.call(t, ctx, saveDashboardToolName, map[string]any{"name": "Usage", "confirmed": false})
	require.Equal(t, analyticsNotEnabledCode, refusalCode(t, err))
}
