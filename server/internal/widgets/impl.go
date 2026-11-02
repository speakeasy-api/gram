package widgets

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/widgets/server"
	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

// maxNameLength matches the widgets table's name check.
const maxNameLength = 200

// copySuffix marks a widget made by duplication.
const copySuffix = " (copy)"

// Service serves widgets: Explore's saved questions, each kept with the
// chart that draws it.
type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	db      *pgxpool.Pool
	auth    *auth.Auth
	authz   *authz.Engine
	audit   *audit.Logger
	catalog *analytics.Catalog
	now     func() time.Time
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, auditLogger *audit.Logger) *Service {
	logger = logger.With(attr.SlogComponent("widgets"))
	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/widgets"),
		logger:  logger,
		db:      db,
		auth:    auth.New(logger, db, sessions, authzEngine),
		authz:   authzEngine,
		audit:   auditLogger,
		catalog: analytics.Default,
		now:     time.Now,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, requestDecoder, goahttp.ResponseEncoder, nil, nil))
}

// requestDecoder decodes JSON request bodies keeping numbers as written. A
// widget's query and visualization are free-form maps, and the default
// decoder routes every number in them through float64, rounding anything
// past 2^53 before it is stored.
func requestDecoder(r *http.Request) goahttp.Decoder {
	dec := goahttp.RequestDecoder(r)
	if jsonDec, ok := dec.(*json.Decoder); ok {
		jsonDec.UseNumber()
	}
	return dec
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// authorize resolves the project a request runs in and checks a scope on it.
// Membership is project:read, which every member holds: saving a chart is
// not an administrative act.
func (s *Service) authorize(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

// view renders a stored widget, validating it as it is read, with the
// dashboards it is placed on.
func (s *Service) view(ctx context.Context, row repo.Widget, now time.Time, dashboards []*gen.WidgetDashboard) *gen.Widget {
	return mv.BuildWidgetView(row, s.validate(ctx, row.Dataset, row.Query, row.Visualization, now), dashboards)
}

// dashboardsOf renders the dashboards a widget is on.
func dashboardsOf(rows []repo.ListDashboardsForWidgetRow) []*gen.WidgetDashboard {
	out := make([]*gen.WidgetDashboard, 0, len(rows))
	for _, row := range rows {
		out = append(out, &gen.WidgetDashboard{ID: row.DashboardID.String(), Name: row.DashboardName})
	}
	return out
}

// usage reads the dashboards one widget is on.
func (s *Service) usage(ctx context.Context, queries *repo.Queries, projectID, widgetID uuid.UUID) ([]*gen.WidgetDashboard, error) {
	rows, err := queries.ListDashboardsForWidget(ctx, repo.ListDashboardsForWidgetParams{ProjectID: projectID, WidgetID: widgetID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list widget dashboards").LogError(ctx, s.logger)
	}
	return dashboardsOf(rows), nil
}

// validate returns what is wrong with a widget, or "", logging a failure
// that is not the widget's fault rather than returning its detail.
func (s *Service) validate(ctx context.Context, dataset string, query, visualization []byte, now time.Time) string {
	reason, err := Validate(s.catalog, dataset, query, visualization, now)
	if err != nil {
		s.logger.ErrorContext(ctx, "validate widget", attr.SlogError(err))
	}
	return reason
}

// problem returns what is wrong with a widget about to be written, or "". A
// failure that is not the widget's fault is a server error rather than a
// rejection the caller could fix.
func (s *Service) problem(ctx context.Context, dataset string, query, visualization []byte) (string, error) {
	reason, err := Validate(s.catalog, dataset, query, visualization, s.now())
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "validate widget").LogError(ctx, s.logger)
	}
	return reason, nil
}

// ListWidgets lists the project's widgets, most recently updated first, each
// validated as it is read.
func (s *Service) ListWidgets(ctx context.Context, _ *gen.ListWidgetsPayload) (*gen.ListWidgetsResult, error) {
	snapshots, err := s.ReadAll(ctx)
	if err != nil {
		return nil, err
	}
	result := &gen.ListWidgetsResult{Widgets: make([]*gen.Widget, 0, len(snapshots))}
	for _, snapshot := range snapshots {
		result.Widgets = append(result.Widgets, snapshot.Widget)
	}
	return result, nil
}

// GetWidget returns one widget, validated as it is read.
func (s *Service) GetWidget(ctx context.Context, payload *gen.GetWidgetPayload) (*gen.Widget, error) {
	snapshot, err := s.Read(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	return snapshot.Widget, nil
}

// CreateWidget saves a widget after validating its query against the
// catalog and its visualization against the query.
func (s *Service) CreateWidget(ctx context.Context, payload *gen.CreateWidgetPayload) (*gen.Widget, error) {
	caller, err := s.Authorize(ctx)
	if err != nil {
		return nil, err
	}
	input := Input{Name: payload.Name, Description: payload.Description, Dataset: payload.Dataset, Query: payload.Query, Visualization: payload.Visualization}
	return inTx(ctx, s, "widget creation", func(tx pgx.Tx) (*gen.Widget, error) {
		snapshot, err := s.CreateInTx(ctx, tx, caller, input)
		return snapshot.Widget, err
	})
}

// DuplicateWidget copies a widget into a new one the caller owns. This is
// how a widget is shared and reused: the copy is the caller's to change
// without redrawing anyone else's. Like every save it is validated, so a
// widget the catalog has broken cannot be copied until it is fixed.
func (s *Service) DuplicateWidget(ctx context.Context, payload *gen.DuplicateWidgetPayload) (*gen.Widget, error) {
	caller, err := s.Authorize(ctx)
	if err != nil {
		return nil, err
	}
	return inTx(ctx, s, "widget duplication", func(tx pgx.Tx) (*gen.Widget, error) {
		snapshot, err := s.DuplicateInTx(ctx, tx, caller, payload.ID, "")
		return snapshot.Widget, err
	})
}

// UpdateWidget replaces a widget's name, description, dataset, query and
// visualization. Its creator can always update it; updating someone else's
// needs project write access, as deleting it does.
func (s *Service) UpdateWidget(ctx context.Context, payload *gen.UpdateWidgetPayload) (*gen.Widget, error) {
	change, err := s.AuthorizeChange(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	input := Input{Name: payload.Name, Description: payload.Description, Dataset: payload.Dataset, Query: payload.Query, Visualization: payload.Visualization}
	return inTx(ctx, s, "widget update", func(tx pgx.Tx) (*gen.Widget, error) {
		snapshot, err := s.UpdateInTx(ctx, tx, change, input, "")
		return snapshot.Widget, err
	})
}

// DeleteWidget soft-deletes a widget. Its creator can always delete it;
// deleting someone else's needs project write access.
func (s *Service) DeleteWidget(ctx context.Context, payload *gen.DeleteWidgetPayload) error {
	change, err := s.AuthorizeChange(ctx, payload.ID)
	if err != nil {
		return err
	}
	_, err = inTx(ctx, s, "widget deletion", func(tx pgx.Tx) ([]*gen.WidgetDashboard, error) {
		return s.DeleteInTx(ctx, tx, change, "")
	})
	return err
}

// requireOwnerOrWrite lets a widget's creator change it with membership
// alone; anyone else needs project write access.
func (s *Service) requireOwnerOrWrite(ctx context.Context, authCtx *contextvalues.AuthContext, creator *string) error {
	if creator != nil && *creator == authCtx.UserID {
		return nil
	}
	return s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil})
}

// checkWidget validates a widget's query against the catalog and its
// visualization against the query, and returns both encoded for storage. A
// query key the server does not know is refused. The payload bytes are what
// is stored, not a re-encoding of the decoded query, which would add zero
// values the client reads as a different query.
func (s *Service) checkWidget(ctx context.Context, dataset string, query, visualization map[string]any) ([]byte, []byte, error) {
	ds, ok := s.catalog.Dataset(dataset)
	if !ok {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "unknown_dataset: dataset %q does not exist", dataset)
	}
	if err := s.requireDataset(ctx, ds); err != nil {
		return nil, nil, err
	}
	encodedQuery, err := json.Marshal(query)
	if err != nil {
		return nil, nil, oops.E(oops.CodeBadRequest, err, "query is not encodable as JSON")
	}
	encodedVisualization, err := json.Marshal(lowercaseChartType(visualization))
	if err != nil {
		return nil, nil, oops.E(oops.CodeBadRequest, err, "visualization is not encodable as JSON")
	}
	reason, err := s.problem(ctx, dataset, encodedQuery, encodedVisualization)
	if err != nil {
		return nil, nil, err
	}
	if reason != "" {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "%s", reason)
	}
	return encodedQuery, encodedVisualization, nil
}

// requireDataset holds a save to what the dataset asks of its readers, so a
// widget over a dataset only organization administrators may read is saved
// by them alone; anyone else could save it but never see it answer.
func (s *Service) requireDataset(ctx context.Context, ds *analytics.Dataset) error {
	return RequireDataset(ctx, s.authz, ds)
}

// RequireDataset refuses the caller a widget over ds unless they may read ds.
// A refusal is a forbidden error wrapping ErrDatasetRestricted. Anything that
// creates widgets, including a dashboard copy, checks this first.
func RequireDataset(ctx context.Context, engine *authz.Engine, ds *analytics.Dataset) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	check, restricted := ds.AccessCheck(authCtx.ActiveOrganizationID)
	if !restricted {
		return nil
	}
	err := engine.Require(ctx, check)
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeForbidden {
		return oops.E(oops.CodeForbidden, errors.Join(ErrDatasetRestricted, err), "only organization administrators can save widgets over %s", ds.Name)
	}
	return err
}

// lowercaseChartType returns visualization with a string type lowercased, so
// the stored type is always in the form the client reads.
func lowercaseChartType(visualization map[string]any) map[string]any {
	chart, ok := visualization["type"].(string)
	if !ok {
		return visualization
	}
	out := maps.Clone(visualization)
	out["type"] = string(ChartType(chart).normalize())
	return out
}

func (s *Service) auditBase(authCtx *contextvalues.AuthContext, row repo.Widget) audit.WidgetEventBase {
	return audit.WidgetEventBase{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        row.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		WidgetURN:        urn.NewWidget(row.ID),
		Name:             row.Name,
	}
}

// copyName names a duplicate "<name> (copy)", shortening the original name
// so the result still fits the column.
func copyName(name string) string {
	room := maxNameLength - utf8.RuneCountInString(copySuffix)
	if utf8.RuneCountInString(name) > room {
		name = string([]rune(name)[:room])
	}
	return name + copySuffix
}
