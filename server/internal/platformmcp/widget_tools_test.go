package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	genwidgets "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	dashboardsrepo "github.com/speakeasy-api/gram/server/internal/dashboards/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/widgets"
)

type widgetToolsFixture struct {
	reg        *Registrar
	service    *WidgetService
	management *widgets.Service
	db         *pgxpool.Pool
	flags      *feature.InMemory
	principal  Principal
	project    ResolvedProject
	logs       *bytes.Buffer
}

// newWidgetToolsFixture registers the widget tools against a service database,
// with Explore on and a caller holding project read and write.
func newWidgetToolsFixture(t *testing.T) (context.Context, *widgetToolsFixture) {
	t.Helper()

	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_widgets")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	principal.ClientID = "widget-tools-test-client"
	principal.Surface = SurfacePlatformMCP
	logger, tracer := testenv.NewLogger(t), testenv.NewTracerProvider(t)
	logs := &bytes.Buffer{}

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagExplore, principal.OrganizationID, true)
	engine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	management := widgets.NewService(logger, tracer, db, &sessions.Manager{}, engine, audit.NewLogger())
	service := NewWidgetService(slog.New(slog.NewJSONHandler(logs, nil)), management, NewPostgresReader(logger, db), engine, flags, NewPostgresOrganizationSlugResolver(db), "test-widget-tools-key")
	require.NotNil(t, service)

	reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
	registerWidgetTools(reg, service)

	return widgetCallerContext(t.Context(), principal, project, authz.ScopeProjectRead, authz.ScopeProjectWrite), &widgetToolsFixture{reg: reg, service: service, management: management, db: db, flags: flags, principal: principal, project: project, logs: logs}
}

func widgetCallerContext(ctx context.Context, principal Principal, project ResolvedProject, scopes ...authz.Scope) context.Context {
	email := principal.UserID + "@example.com"
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, Email: &email}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextWithPrincipal(ctx, principal)
	grants := make([]authz.Grant, 0, len(scopes))
	for _, scope := range scopes {
		grants = append(grants, authz.NewGrant(scope, project.ID.String()))
	}
	return authz.GrantsToContext(ctx, grants)
}

func (f *widgetToolsFixture) invoke(t *testing.T, ctx context.Context, tool string, arguments any) (any, error) {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	return descriptorByName(t, f.reg, tool).Invoke(ctx, encoded)
}

func widgetCall[Out any](t *testing.T, f *widgetToolsFixture, ctx context.Context, tool string, arguments any) Out {
	t.Helper()
	raw, err := f.invoke(t, ctx, tool, arguments)
	require.NoError(t, err)
	out, ok := raw.(Out)
	require.True(t, ok)
	return out
}

func (f *widgetToolsFixture) write(t *testing.T, ctx context.Context, tool string, arguments any) widgetWriteOutput {
	t.Helper()
	return widgetCall[widgetWriteOutput](t, f, ctx, tool, arguments)
}

// scopedContext is the context the widgets service sees from the tools: the
// caller's auth with the project selected.
func (f *widgetToolsFixture) scopedContext(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	scoped, _, err := f.service.scope(ctx, f.principal, f.project.ID.String())
	require.NoError(t, err)
	return scoped
}

// createDirectly saves a widget as the dashboard would.
func (f *widgetToolsFixture) createDirectly(t *testing.T, ctx context.Context, name string) widgets.Snapshot {
	t.Helper()
	scoped := f.scopedContext(t, ctx)
	caller, err := f.management.Authorize(scoped)
	require.NoError(t, err)
	tx, err := f.db.Begin(scoped) //nolint:glint // notestingrawsql: transaction boundary only; the widget is written by the widgets service's create core.
	require.NoError(t, err)
	created, err := f.management.CreateInTx(scoped, tx, caller, widgets.Input{Name: name, Description: nil, Dataset: "tool_calls", Query: widgetTestQuery(), Visualization: widgetTestChart()})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(scoped))
	return created
}

func widgetTestQuery() map[string]any {
	return map[string]any{
		"window":     "7d",
		"grain":      "day",
		"dimensions": []any{"user"},
		"measures":   []any{map[string]any{"op": "count"}},
		"limit":      50,
	}
}

func widgetTestChart() map[string]any {
	return map[string]any{"type": "bar", "options": map[string]any{}}
}

func requireWidgetRefusal(t *testing.T, err error) widgetRefusal {
	t.Helper()
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	var decoded widgetRefusal
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &decoded))
	require.Equal(t, widgetsFeature, decoded.Feature)
	return decoded
}

func saveArguments(projectID string, name string) map[string]any {
	return map[string]any{"project_id": projectID, "name": name, "dataset": "tool_calls", "query": widgetTestQuery(), "visualization": widgetTestChart(), "confirmed": false}
}

// previewOutput is a write preview as its confirmation carries it: the
// version and token the preview returned.
type previewOutput interface {
	previewed() (version, token string)
}

func (o widgetWriteOutput) previewed() (version, token string) { return o.Version, o.PreviewToken }

// confirmed is the confirmation of a preview under the given key: the
// previewed arguments with the preview's version and token.
func confirmed(arguments map[string]any, preview previewOutput, key string) map[string]any {
	version, token := preview.previewed()
	out := maps.Clone(arguments)
	out["confirmed"] = true
	out["expected_version"] = version
	out["preview_token"] = token
	out["idempotency_key"] = key
	return out
}

func TestWidgetToolsDeclareTheirContract(t *testing.T) {
	t.Parallel()

	reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
	registerWidgetTools(reg, nil)
	for _, name := range []string{listWidgetsToolName, getWidgetToolName} {
		descriptor := descriptorByName(t, reg, name)
		require.True(t, descriptor.Annotations.ReadOnlyHint, name)
		require.Equal(t, discoveryProjectRead, descriptor.Meta.DiscoveryScopes, name)
	}
	// Writes are discovered on project read too; the service decides who may change what.
	for _, name := range []string{saveWidgetToolName, duplicateWidgetToolName, deleteWidgetToolName} {
		descriptor := descriptorByName(t, reg, name)
		require.False(t, descriptor.Annotations.ReadOnlyHint, name)
		require.Equal(t, discoveryProjectRead, descriptor.Meta.DiscoveryScopes, name)
		// A create or a copy makes another widget per confirmed key; only a
		// delete leaves the project as it left it.
		require.Equal(t, name == deleteWidgetToolName, descriptor.Annotations.IdempotentHint, name)
	}
	projectID, widgetID := uuid.NewString(), uuid.NewString()
	target := map[string]any{"project_id": projectID, "widget_id": widgetID, "confirmed": false}
	for name, arguments := range map[string]map[string]any{
		listWidgetsToolName:     {"project_id": projectID},
		getWidgetToolName:       {"project_id": projectID, "widget_id": widgetID},
		saveWidgetToolName:      saveArguments(projectID, "Sessions"),
		duplicateWidgetToolName: target,
		deleteWidgetToolName:    target,
	} {
		descriptor := descriptorByName(t, reg, name)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences, name)
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization, name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope, name)

		encoded, err := json.Marshal(arguments)
		require.NoError(t, err)
		_, err = descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), encoded)
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal, "%s without a service refuses readably", name)
		require.Equal(t, unavailableCode, refusal.Code, name)
	}
}

func TestWidgetToolsRefuseWhenExploreIsOff(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	f.flags.SetFlag(feature.FlagExplore, f.principal.OrganizationID, false)

	_, err := f.invoke(t, ctx, listWidgetsToolName, map[string]any{"project_id": f.project.ID.String()})
	refusal := requireWidgetRefusal(t, err)
	require.Equal(t, analyticsNotEnabledCode, refusal.Code)
	require.Contains(t, refusal.Message, "not switched on")

	_, err = f.invoke(t, ctx, saveWidgetToolName, saveArguments(f.project.ID.String(), "Tool calls by user"))
	require.Equal(t, analyticsNotEnabledCode, requireWidgetRefusal(t, err).Code)
}

func TestWidgetToolsCheckProjectAccessBeforeAnyRead(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	created := f.createDirectly(t, ctx, "Existing")

	noAccess := widgetCallerContext(ctx, f.principal, f.project)
	for _, call := range []struct {
		tool      string
		arguments map[string]any
	}{
		{listWidgetsToolName, map[string]any{"project_id": f.project.ID.String()}},
		{getWidgetToolName, map[string]any{"project_id": f.project.ID.String(), "widget_id": created.Widget.ID}},
		// A project that does not exist reads the same as one the caller cannot see.
		{listWidgetsToolName, map[string]any{"project_id": uuid.NewString()}},
	} {
		_, err := f.invoke(t, noAccess, call.tool, call.arguments)
		require.Equal(t, "permission_denied", requireWidgetRefusal(t, err).Code, call.tool)
	}

	// Writes are refused the same way when the caller cannot read the project.
	for _, tool := range []string{saveWidgetToolName, duplicateWidgetToolName, deleteWidgetToolName} {
		arguments := map[string]any{"project_id": f.project.ID.String(), "widget_id": created.Widget.ID, "confirmed": false}
		if tool == saveWidgetToolName {
			arguments = saveArguments(f.project.ID.String(), "New")
		}
		_, err := f.invoke(t, noAccess, tool, arguments)
		require.Equal(t, "permission_denied", requireWidgetRefusal(t, err).Code, tool)
	}
}

func TestAMemberCreatesDuplicatesAndChangesTheirOwnWidgetsWithReadAccess(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	member := widgetCallerContext(ctx, f.principal, f.project, authz.ScopeProjectRead)

	create := saveArguments(f.project.ID.String(), "Mine")
	created := f.write(t, member, saveWidgetToolName, confirmed(create, f.write(t, member, saveWidgetToolName, create), "member-create"))
	require.Equal(t, widgetSnapshotCurrent, created.SnapshotScope)

	duplicate := map[string]any{"project_id": f.project.ID.String(), "widget_id": created.Widget.ID, "confirmed": false}
	copied := f.write(t, member, duplicateWidgetToolName, confirmed(duplicate, f.write(t, member, duplicateWidgetToolName, duplicate), "member-duplicate"))
	require.Equal(t, "Mine (copy)", copied.Widget.Name)

	update := saveArguments(f.project.ID.String(), "Mine, renamed")
	update["widget_id"] = created.Widget.ID
	updated := f.write(t, member, saveWidgetToolName, confirmed(update, f.write(t, member, saveWidgetToolName, update), "member-update"))
	require.Equal(t, "Mine, renamed", updated.Widget.Name)

	remove := map[string]any{"project_id": f.project.ID.String(), "widget_id": created.Widget.ID, "confirmed": false}
	deleted := f.write(t, member, deleteWidgetToolName, confirmed(remove, f.write(t, member, deleteWidgetToolName, remove), "member-delete"))
	require.Equal(t, widgetSnapshotDeleted, deleted.SnapshotScope)
}

func TestSaveWidgetPreviewsThenCreatesOnceAndReplays(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	arguments := saveArguments(f.project.ID.String(), "Tool calls by user")

	preview := f.write(t, ctx, saveWidgetToolName, arguments)
	require.True(t, preview.Preview)
	require.Equal(t, operationCreateWidget, preview.Operation)
	require.Equal(t, newWidgetVersion, preview.Version)
	require.NotEmpty(t, preview.PreviewToken)
	require.Equal(t, widgetSnapshotPreview, preview.SnapshotScope)
	require.Equal(t, "Tool calls by user", preview.Widget.Name)

	listed := widgetCall[listWidgetsOutput](t, f, ctx, listWidgetsToolName, map[string]any{"project_id": f.project.ID.String()})
	require.Empty(t, listed.Widgets, "a preview saves nothing")

	request := confirmed(arguments, preview, "create-tool-calls-by-user")
	created := f.write(t, ctx, saveWidgetToolName, request)
	require.False(t, created.Preview)
	require.False(t, created.Replayed)
	require.Equal(t, widgetSnapshotCurrent, created.SnapshotScope)
	require.NotEmpty(t, created.Widget.ID)
	require.NotEmpty(t, created.ReceiptID)
	require.Equal(t, created.Widget.Version, created.Version)

	got := widgetCall[getWidgetOutput](t, f, ctx, getWidgetToolName, map[string]any{"project_id": f.project.ID.String(), "widget_id": created.Widget.ID}).Widget
	require.Equal(t, "Tool calls by user", got.Name)
	require.Equal(t, "tool_calls", got.Dataset)
	require.Equal(t, "bar", got.Visualization["type"])
	require.Nil(t, got.InvalidReason)
	require.Equal(t, created.Version, got.Version)

	replayed := f.write(t, ctx, saveWidgetToolName, request)
	require.True(t, replayed.Replayed)
	require.Equal(t, created.Widget.ID, replayed.Widget.ID)
	require.Equal(t, created.ReceiptID, replayed.ReceiptID)

	listed = widgetCall[listWidgetsOutput](t, f, ctx, listWidgetsToolName, map[string]any{"project_id": f.project.ID.String()})
	require.Len(t, listed.Widgets, 1, "the replay wrote nothing")
	require.Equal(t, "bar", listed.Widgets[0].ChartType)
	require.Equal(t, &f.principal.UserID, listed.Widgets[0].CreatedByUserID)
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionWidgetCreate)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	// The same key for a different request is a conflict, not a replay.
	other := saveArguments(f.project.ID.String(), "Something else")
	otherPreview := f.write(t, ctx, saveWidgetToolName, other)
	_, err = f.invoke(t, ctx, saveWidgetToolName, confirmed(other, otherPreview, "create-tool-calls-by-user"))
	require.Equal(t, "conflict", requireWidgetRefusal(t, err).Code)
}

func TestAWidgetPreviewCommitsUnderOneKey(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	existing := f.createDirectly(t, ctx, "Source")

	create := saveArguments(f.project.ID.String(), "Once")
	duplicate := map[string]any{"project_id": f.project.ID.String(), "widget_id": existing.Widget.ID, "confirmed": false}
	for tool, arguments := range map[string]map[string]any{saveWidgetToolName: create, duplicateWidgetToolName: duplicate} {
		preview := f.write(t, ctx, tool, arguments)
		first := f.write(t, ctx, tool, confirmed(arguments, preview, tool+"-first"))

		_, err := f.invoke(t, ctx, tool, confirmed(arguments, preview, tool+"-second"))
		refusal := requireWidgetRefusal(t, err)
		require.Equal(t, "preview_used", refusal.Code, tool)
		require.Equal(t, "preview_token", refusal.Field, tool)

		replayed := f.write(t, ctx, tool, confirmed(arguments, preview, tool+"-first"))
		require.True(t, replayed.Replayed, tool)
		require.Equal(t, first.Widget.ID, replayed.Widget.ID, tool)

		again := f.write(t, ctx, tool, arguments)
		require.NotEqual(t, preview.PreviewToken, again.PreviewToken, "every preview is distinct")
		f.write(t, ctx, tool, confirmed(arguments, again, tool+"-again"))
	}

	listed := widgetCall[listWidgetsOutput](t, f, ctx, listWidgetsToolName, map[string]any{"project_id": f.project.ID.String()})
	require.Len(t, listed.Widgets, 5, "the source, plus two creates and two copies")
}

func TestAWidgetPreviewExpiresWithItsReceipt(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	arguments := saveArguments(f.project.ID.String(), "Late")
	preview := f.write(t, ctx, saveWidgetToolName, arguments)

	f.service.now = func() time.Time { return time.Now().Add(receiptLifetime + time.Minute) }
	_, err := f.invoke(t, ctx, saveWidgetToolName, confirmed(arguments, preview, "late"))
	refusal := requireWidgetRefusal(t, err)
	require.Equal(t, "preview_expired", refusal.Code)
	require.Equal(t, "preview_token", refusal.Field)
}

func TestSaveWidgetConfirmationNeedsItsPreview(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	arguments := saveArguments(f.project.ID.String(), "Tool calls by user")
	preview := f.write(t, ctx, saveWidgetToolName, arguments)
	request := confirmed(arguments, preview, "a-key")

	for field, mutate := range map[string]func(map[string]any){
		"idempotency_key":  func(r map[string]any) { delete(r, "idempotency_key") },
		"expected_version": func(r map[string]any) { delete(r, "expected_version") },
		"preview_token":    func(r map[string]any) { delete(r, "preview_token") },
	} {
		changed := confirmed(arguments, preview, "a-key")
		mutate(changed)
		_, err := f.invoke(t, ctx, saveWidgetToolName, changed)
		refusal := requireWidgetRefusal(t, err)
		require.Equal(t, "invalid_request", refusal.Code, field)
		require.Equal(t, field, refusal.Field)
	}

	changed := confirmed(arguments, preview, "a-key")
	changed["name"] = "Not what was previewed"
	_, err := f.invoke(t, ctx, saveWidgetToolName, changed)
	require.Equal(t, "preview_mismatch", requireWidgetRefusal(t, err).Code)

	_, err = f.invoke(t, ctx, saveWidgetToolName, request)
	require.NoError(t, err, "the untouched confirmation still commits")
}

func TestSaveWidgetSurfacesTheValidationReason(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)

	arguments := saveArguments(f.project.ID.String(), "Broken")
	arguments["dataset"] = "no_such_dataset"
	_, err := f.invoke(t, ctx, saveWidgetToolName, arguments)
	refusal := requireWidgetRefusal(t, err)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Contains(t, refusal.Message, "unknown_dataset")

	arguments = saveArguments(f.project.ID.String(), "")
	_, err = f.invoke(t, ctx, saveWidgetToolName, arguments)
	refusal = requireWidgetRefusal(t, err)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Contains(t, refusal.Message, "invalid_name", "the widgets service's own limit applies")
}

func TestSaveWidgetRefusesAStaleVersion(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	existing := f.createDirectly(t, ctx, "Original")

	arguments := saveArguments(f.project.ID.String(), "Renamed by the agent")
	arguments["widget_id"] = existing.Widget.ID
	preview := f.write(t, ctx, saveWidgetToolName, arguments)
	require.Equal(t, operationUpdateWidget, preview.Operation)
	require.Equal(t, existing.Version, preview.Version)
	require.Equal(t, "Original", preview.Current.Name)

	// Someone edits the widget in the dashboard between preview and confirm.
	scoped := f.scopedContext(t, ctx)
	_, err := f.management.UpdateWidget(scoped, &genwidgets.UpdateWidgetPayload{ID: existing.Widget.ID, Name: "Renamed in the dashboard", Description: nil, Dataset: "tool_calls", Query: widgetTestQuery(), Visualization: widgetTestChart(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	_, err = f.invoke(t, ctx, saveWidgetToolName, confirmed(arguments, preview, "rename"))
	refusal := requireWidgetRefusal(t, err)
	require.Equal(t, "version_changed", refusal.Code)
	require.Equal(t, "expected_version", refusal.Field)

	current, err := f.management.Read(scoped, existing.Widget.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed in the dashboard", current.Widget.Name)

	preview = f.write(t, ctx, saveWidgetToolName, arguments)
	updated := f.write(t, ctx, saveWidgetToolName, confirmed(arguments, preview, "rename-again"))
	require.Equal(t, "Renamed by the agent", updated.Widget.Name)
	require.NotEqual(t, preview.Version, updated.Version)
}

func TestWidgetWritesRefuseANonCreatorWithoutProjectWrite(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	existing := f.createDirectly(t, ctx, "Someone else's")
	other := f.principal
	other.UserID = "user_" + uuid.NewString()
	member := widgetCallerContext(ctx, other, f.project, authz.ScopeProjectRead)

	update := saveArguments(f.project.ID.String(), "Hijacked")
	update["widget_id"] = existing.Widget.ID
	remove := map[string]any{"project_id": f.project.ID.String(), "widget_id": existing.Widget.ID, "confirmed": false}

	// The member can read the project; the creator-or-write rule refuses the previews.
	_, err := f.invoke(t, member, listWidgetsToolName, map[string]any{"project_id": f.project.ID.String()})
	require.NoError(t, err)
	for tool, arguments := range map[string]map[string]any{saveWidgetToolName: update, deleteWidgetToolName: remove} {
		_, err := f.invoke(t, member, tool, arguments)
		refusal := requireWidgetRefusal(t, err)
		require.Equal(t, widgetOwnerOrWriteRequired, refusal.Code, tool)
		require.Equal(t, "widget_id", refusal.Field, tool)
	}

	// A correctly signed confirmation is still refused under the row lock.
	project := ResolvedProject{ID: f.project.ID, Name: f.project.Name, Slug: f.project.Slug}
	for tool, request := range map[string]struct {
		arguments map[string]any
		operation widgetOperation
		proposal  widgetTokenProposal
	}{
		saveWidgetToolName:   {update, operationUpdateWidget, widgetTokenProposal{WidgetID: existing.Widget.ID, Widget: &widgets.Input{Name: "Hijacked", Description: nil, Dataset: "tool_calls", Query: widgetTestQuery(), Visualization: widgetTestChart()}}},
		deleteWidgetToolName: {remove, operationDeleteWidget, widgetTokenProposal{WidgetID: existing.Widget.ID, Widget: nil}},
	} {
		token, err := f.service.previewToken(strconv.FormatInt(time.Now().Unix(), 10)+"-test", other, project, request.operation, existing.Version, request.proposal)
		require.NoError(t, err)
		arguments := confirmed(request.arguments, widgetWriteOutput{Version: existing.Version, PreviewToken: token}, "member-"+tool)
		_, err = f.invoke(t, member, tool, arguments)
		require.Equal(t, widgetOwnerOrWriteRequired, requireWidgetRefusal(t, err).Code, tool)
	}

	current, err := f.management.Read(f.scopedContext(t, ctx), existing.Widget.ID)
	require.NoError(t, err)
	require.Equal(t, "Someone else's", current.Widget.Name)

	// The same member may still copy it, and change the copy.
	duplicate := map[string]any{"project_id": f.project.ID.String(), "widget_id": existing.Widget.ID, "confirmed": false}
	copied := f.write(t, member, duplicateWidgetToolName, confirmed(duplicate, f.write(t, member, duplicateWidgetToolName, duplicate), "member-copy"))
	require.Equal(t, &other.UserID, copied.Widget.CreatedByUserID)
}

func TestDeleteWidgetReportsTheDashboardsThatLostIt(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	existing := f.createDirectly(t, ctx, "Placed")

	dashboards := dashboardsrepo.New(f.db)
	dashboard, err := dashboards.CreateDashboard(ctx, dashboardsrepo.CreateDashboardParams{
		ProjectID: f.project.ID, OrganizationID: f.principal.OrganizationID, CreatedByUserID: pgtype.Text{String: f.principal.UserID, Valid: true},
		Name: "Ops", Description: pgtype.Text{String: "", Valid: false}, Filters: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = dashboards.InsertPlacement(ctx, dashboardsrepo.InsertPlacementParams{
		ProjectID: f.project.ID, OrganizationID: f.principal.OrganizationID, DashboardID: dashboard.ID, WidgetID: uuid.MustParse(existing.Widget.ID), X: 0, Y: 0, W: 4, H: 3,
	})
	require.NoError(t, err)

	arguments := map[string]any{"project_id": f.project.ID.String(), "widget_id": existing.Widget.ID, "confirmed": false}
	preview := f.write(t, ctx, deleteWidgetToolName, arguments)
	require.Equal(t, []widgetDashboardRef{{ID: dashboard.ID.String(), Name: "Ops"}}, preview.Dashboards)

	deleted := f.write(t, ctx, deleteWidgetToolName, confirmed(arguments, preview, "delete-placed"))
	require.Equal(t, []widgetDashboardRef{{ID: dashboard.ID.String(), Name: "Ops"}}, deleted.Dashboards)
	require.Equal(t, widgetSnapshotDeleted, deleted.SnapshotScope)

	replayed := f.write(t, ctx, deleteWidgetToolName, confirmed(arguments, preview, "delete-placed"))
	require.True(t, replayed.Replayed)
	require.Equal(t, deleted.Dashboards, replayed.Dashboards, "a replay reports the dashboards from its receipt")

	_, err = f.invoke(t, ctx, getWidgetToolName, map[string]any{"project_id": f.project.ID.String(), "widget_id": existing.Widget.ID})
	require.Equal(t, "not_found", requireWidgetRefusal(t, err).Code)
}

func TestDeleteWidgetRefusesAStaleVersion(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	existing := f.createDirectly(t, ctx, "Original")

	arguments := map[string]any{"project_id": f.project.ID.String(), "widget_id": existing.Widget.ID, "confirmed": false}
	preview := f.write(t, ctx, deleteWidgetToolName, arguments)

	scoped := f.scopedContext(t, ctx)
	_, err := f.management.UpdateWidget(scoped, &genwidgets.UpdateWidgetPayload{ID: existing.Widget.ID, Name: "Edited meanwhile", Description: nil, Dataset: "tool_calls", Query: widgetTestQuery(), Visualization: widgetTestChart(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	_, err = f.invoke(t, ctx, deleteWidgetToolName, confirmed(arguments, preview, "delete-stale"))
	require.Equal(t, "version_changed", requireWidgetRefusal(t, err).Code)
	_, err = f.management.Read(scoped, existing.Widget.ID)
	require.NoError(t, err, "the widget was not deleted")
}

// unreadableWidgets fails every read, as a post-commit verification read can.
type unreadableWidgets struct{ widgetManagement }

func (unreadableWidgets) Read(context.Context, string) (widgets.Snapshot, error) {
	return widgets.Snapshot{}, errors.New("read replica unavailable")
}

func TestSaveWidgetReportsAnUnverifiedCommit(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	service := f.service.widgets
	f.service.widgets = unreadableWidgets{widgetManagement: service}

	arguments := saveArguments(f.project.ID.String(), "Committed blind")
	preview := f.write(t, ctx, saveWidgetToolName, arguments)
	committed := f.write(t, ctx, saveWidgetToolName, confirmed(arguments, preview, "blind"))
	require.Equal(t, widgetSnapshotVerificationUnavailable, committed.SnapshotScope)
	require.NotEmpty(t, committed.ReceiptID)
	require.Nil(t, committed.Widget)

	all, err := service.ReadRecent(f.scopedContext(t, ctx), 10)
	require.NoError(t, err)
	require.Len(t, all, 1, "the write committed")
}

func TestSaveWidgetReplayReportsAWidgetDeletedSince(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	arguments := saveArguments(f.project.ID.String(), "Short-lived")
	request := confirmed(arguments, f.write(t, ctx, saveWidgetToolName, arguments), "short-lived")
	created := f.write(t, ctx, saveWidgetToolName, request)

	require.NoError(t, f.management.DeleteWidget(f.scopedContext(t, ctx), &genwidgets.DeleteWidgetPayload{ID: created.Widget.ID, SessionToken: nil, ProjectSlugInput: nil}))

	replayed := f.write(t, ctx, saveWidgetToolName, request)
	require.True(t, replayed.Replayed)
	require.Equal(t, widgetSnapshotDeleted, replayed.SnapshotScope)
	require.Nil(t, replayed.Widget)
}

func TestDuplicateWidgetCopiesIntoTheCaller(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	existing := f.createDirectly(t, ctx, "Source")

	arguments := map[string]any{"project_id": f.project.ID.String(), "widget_id": existing.Widget.ID, "confirmed": false}
	preview := f.write(t, ctx, duplicateWidgetToolName, arguments)
	require.Equal(t, existing.Version, preview.Version)
	require.Equal(t, "Source", preview.Widget.Name)

	copied := f.write(t, ctx, duplicateWidgetToolName, confirmed(arguments, preview, "copy-source"))
	require.Equal(t, "Source (copy)", copied.Widget.Name)
	require.NotEqual(t, existing.Widget.ID, copied.Widget.ID)
	require.Equal(t, widgetSnapshotCurrent, copied.SnapshotScope)
}

type failingWidgets struct{ widgetManagement }

func (failingWidgets) ReadRecent(context.Context, int32) ([]widgets.Snapshot, error) {
	return nil, errors.New("pq: relation widgets is on fire")
}

func TestWidgetToolsHideUnexpectedFailures(t *testing.T) {
	t.Parallel()
	ctx, f := newWidgetToolsFixture(t)
	f.service.widgets = failingWidgets{widgetManagement: f.service.widgets}

	_, err := f.invoke(t, ctx, listWidgetsToolName, map[string]any{"project_id": f.project.ID.String()})
	refusal := requireWidgetRefusal(t, err)
	require.Equal(t, unavailableCode, refusal.Code)
	require.NotContains(t, refusal.Message, "on fire")
	require.Contains(t, f.logs.String(), "on fire", "the cause is logged for operators")
}
