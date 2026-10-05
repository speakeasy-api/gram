package widgets

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"sync"
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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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
	presets func() (map[string]mv.PresetPageSource, error)
	now     func() time.Time
}

// builtinPresets parses the embedded presets once. TestPresets keeps a
// broken file from shipping, so a failure here is a server error.
var builtinPresets = sync.OnceValues(func() (map[string]mv.PresetPageSource, error) {
	return parsePresets(presetsJSON)
})

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
		presets: builtinPresets,
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

// view renders a stored widget, validating it as it is read.
func (s *Service) view(ctx context.Context, row repo.Widget, now time.Time) *gen.Widget {
	return mv.BuildWidgetView(row, s.validate(ctx, row.Dataset, row.Query, row.Visualization, now))
}

// validate returns what is wrong with a widget, or "", logging a failure
// that is not the widget's fault rather than returning its detail.
func (s *Service) validate(ctx context.Context, dataset string, query, visualization []byte, now time.Time) string {
	reason, err := validate(s.catalog, dataset, query, visualization, now)
	if err != nil {
		s.logger.ErrorContext(ctx, "validate widget", attr.SlogError(err))
	}
	return reason
}

// problem returns what is wrong with a widget about to be written, or "". A
// failure that is not the widget's fault is a server error rather than a
// rejection the caller could fix.
func (s *Service) problem(ctx context.Context, dataset string, query, visualization []byte) (string, error) {
	reason, err := validate(s.catalog, dataset, query, visualization, s.now())
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "validate widget").LogError(ctx, s.logger)
	}
	return reason, nil
}

// ListWidgets lists the project's widgets, most recently updated first, each
// validated as it is read.
func (s *Service) ListWidgets(ctx context.Context, _ *gen.ListWidgetsPayload) (*gen.ListWidgetsResult, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListWidgets(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list widgets").LogError(ctx, s.logger)
	}

	now := s.now()
	result := &gen.ListWidgetsResult{Widgets: make([]*gen.Widget, 0, len(rows))}
	for _, row := range rows {
		result.Widgets = append(result.Widgets, s.view(ctx, row, now))
	}
	return result, nil
}

// GetPreset returns a product page's preset layout of widgets, each
// validated as it is read, as a saved widget is.
func (s *Service) GetPreset(ctx context.Context, payload *gen.GetPresetPayload) (*gen.WidgetPreset, error) {
	if _, err := s.authorize(ctx, authz.ScopeProjectRead); err != nil {
		return nil, err
	}
	pages, err := s.presets()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load widget presets").LogError(ctx, s.logger)
	}
	page, ok := pages[payload.Page]
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "no preset for page %q", payload.Page)
	}
	now := s.now()
	return mv.BuildWidgetPresetView(page, func(dataset string, query, visualization []byte) string {
		return s.validate(ctx, dataset, query, visualization, now)
	}), nil
}

// GetWidget returns one widget, validated as it is read.
func (s *Service) GetWidget(ctx context.Context, payload *gen.GetWidgetPayload) (*gen.Widget, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid widget id")
	}

	row, err := repo.New(s.db).GetWidget(ctx, repo.GetWidgetParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get widget").LogError(ctx, s.logger)
	}
	return s.view(ctx, row, s.now()), nil
}

// CreateWidget saves a widget after validating its query against the
// catalog and its visualization against the query.
func (s *Service) CreateWidget(ctx context.Context, payload *gen.CreateWidgetPayload) (*gen.Widget, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}

	query, visualization, err := s.checkWidget(ctx, payload.Dataset, payload.Query, payload.Visualization)
	if err != nil {
		return nil, err
	}

	return s.insert(ctx, authCtx, repo.CreateWidgetParams{
		ProjectID:       *authCtx.ProjectID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		CreatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		Name:            payload.Name,
		Description:     conv.PtrToPGTextEmpty(payload.Description),
		Dataset:         payload.Dataset,
		Query:           query,
		Visualization:   visualization,
	}, nil)
}

// DuplicateWidget copies a widget into a new one the caller owns. This is
// how a widget is shared and reused: the copy is the caller's to change
// without redrawing anyone else's. Like every save it is validated, so a
// widget the catalog has broken cannot be copied until it is fixed.
func (s *Service) DuplicateWidget(ctx context.Context, payload *gen.DuplicateWidgetPayload) (*gen.Widget, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid widget id")
	}

	source, err := repo.New(s.db).GetWidget(ctx, repo.GetWidgetParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load widget").LogError(ctx, s.logger)
	}
	reason, err := s.problem(ctx, source.Dataset, source.Query, source.Visualization)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "widget cannot be duplicated until it is fixed: %s", reason)
	}

	sourceURN := urn.NewWidget(source.ID)
	return s.insert(ctx, authCtx, repo.CreateWidgetParams{
		ProjectID:       *authCtx.ProjectID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		CreatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		Name:            copyName(source.Name),
		Description:     source.Description,
		Dataset:         source.Dataset,
		Query:           source.Query,
		Visualization:   source.Visualization,
	}, &sourceURN)
}

// insert stores a new widget and audits it, in one transaction.
func (s *Service) insert(ctx context.Context, authCtx *contextvalues.AuthContext, params repo.CreateWidgetParams, duplicatedFrom *urn.Widget) (*gen.Widget, error) {
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "saving a widget requires a user identity")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin widget creation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateWidget(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create widget").LogError(ctx, s.logger)
	}

	view := mv.BuildWidgetView(row, "")
	if err := s.audit.LogWidgetCreate(ctx, dbtx, audit.LogWidgetCreateEvent{WidgetEventBase: s.auditBase(authCtx, row), Snapshot: view, DuplicatedFrom: duplicatedFrom}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit widget creation").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit widget creation").LogError(ctx, s.logger)
	}
	return view, nil
}

// UpdateWidget replaces a widget's name, description, dataset, query and
// visualization. Its creator can always update it; updating someone else's
// needs project write access, as deleting it does.
func (s *Service) UpdateWidget(ctx context.Context, payload *gen.UpdateWidgetPayload) (*gen.Widget, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid widget id")
	}

	query, visualization, err := s.checkWidget(ctx, payload.Dataset, payload.Query, payload.Visualization)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin widget update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetWidgetForUpdate(ctx, repo.GetWidgetForUpdateParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load widget").LogError(ctx, s.logger)
	}
	if err := s.requireOwnerOrWrite(ctx, authCtx, before); err != nil {
		return nil, err
	}

	row, err := queries.UpdateWidget(ctx, repo.UpdateWidgetParams{
		Name:          payload.Name,
		Description:   conv.PtrToPGTextEmpty(payload.Description),
		Dataset:       payload.Dataset,
		Query:         query,
		Visualization: visualization,
		ProjectID:     *authCtx.ProjectID,
		ID:            id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update widget").LogError(ctx, s.logger)
	}

	view := mv.BuildWidgetView(row, "")
	if err := s.audit.LogWidgetUpdate(ctx, dbtx, audit.LogWidgetUpdateEvent{WidgetEventBase: s.auditBase(authCtx, row), Before: s.view(ctx, before, s.now()), After: view}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit widget update").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit widget update").LogError(ctx, s.logger)
	}
	return view, nil
}

// DeleteWidget soft-deletes a widget. Its creator can always delete it;
// deleting someone else's needs project write access.
func (s *Service) DeleteWidget(ctx context.Context, payload *gen.DeleteWidgetPayload) error {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid widget id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin widget deletion").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	existing, err := queries.GetWidgetForUpdate(ctx, repo.GetWidgetForUpdateParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return oops.E(oops.CodeUnexpected, err, "load widget").LogError(ctx, s.logger)
	}
	if err := s.requireOwnerOrWrite(ctx, authCtx, existing); err != nil {
		return err
	}

	row, err := queries.DeleteWidget(ctx, repo.DeleteWidgetParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return oops.E(oops.CodeUnexpected, err, "delete widget").LogError(ctx, s.logger)
	}
	if err := s.audit.LogWidgetDelete(ctx, dbtx, audit.LogWidgetDeleteEvent{WidgetEventBase: s.auditBase(authCtx, row)}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit widget deletion").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit widget deletion").LogError(ctx, s.logger)
	}
	return nil
}

// requireOwnerOrWrite lets a widget's creator change it with membership
// alone; anyone else needs project write access.
func (s *Service) requireOwnerOrWrite(ctx context.Context, authCtx *contextvalues.AuthContext, widget repo.Widget) error {
	if creator := conv.FromPGText[string](widget.CreatedByUserID); creator != nil && *creator == authCtx.UserID {
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
	if _, ok := s.catalog.Dataset(dataset); !ok {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "unknown_dataset: dataset %q does not exist", dataset)
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
