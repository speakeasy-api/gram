package dashboards

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	srv "github.com/speakeasy-api/gram/server/gen/http/dashboards/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dashboards/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/widgets"
	widgetsrepo "github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

// maxNameLength and maxDescriptionLength are the longest a dashboard's name
// and description may be. The API enforces them too; the table does not, so
// they can move without a migration.
const (
	maxNameLength        = 200
	maxDescriptionLength = 2000
)

// details is a dashboard's name and description as they are stored: both
// trimmed, and a description that says nothing dropped.
type details struct {
	name        string
	description *string
}

// checkDetails returns the name and description to store, or why they
// cannot be.
func checkDetails(name string, description *string) (details, string) {
	out := details{name: strings.TrimSpace(name), description: nil}
	if description != nil {
		if trimmed := strings.TrimSpace(*description); trimmed != "" {
			out.description = &trimmed
		}
	}
	switch {
	case out.name == "":
		return out, "a dashboard needs a name"
	case utf8.RuneCountInString(out.name) > maxNameLength:
		return out, fmt.Sprintf("a dashboard name is at most %d characters", maxNameLength)
	case out.description != nil && utf8.RuneCountInString(*out.description) > maxDescriptionLength:
		return out, fmt.Sprintf("a dashboard description is at most %d characters", maxDescriptionLength)
	}
	return out, ""
}

// copySuffix marks a dashboard made by duplication.
const copySuffix = " (copy)"

// Service serves dashboards: a project's layouts of saved widgets.
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
	logger = logger.With(attr.SlogComponent("dashboards"))
	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/dashboards"),
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
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// authorize resolves the project a request runs in and checks a scope on it.
// Membership is project:read, which every member holds: making a dashboard
// is not an administrative act.
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

// requireOwnerOrWrite lets a dashboard's creator change it with membership
// alone; anyone else needs project write access.
func (s *Service) requireOwnerOrWrite(ctx context.Context, authCtx *contextvalues.AuthContext, dashboard repo.Dashboard) error {
	if creator := conv.FromPGText[string](dashboard.CreatedByUserID); creator != nil && *creator == authCtx.UserID {
		return nil
	}
	return s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil})
}

// ListDashboards lists the project's dashboards, most recently updated
// first, each with its cards.
func (s *Service) ListDashboards(ctx context.Context, _ *gen.ListDashboardsPayload) (*gen.ListDashboardsResult, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	queries := repo.New(s.db)

	rows, err := queries.ListDashboards(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list dashboards").LogError(ctx, s.logger)
	}
	placements, err := queries.ListProjectPlacements(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list dashboard cards").LogError(ctx, s.logger)
	}
	byDashboard := map[uuid.UUID][]repo.DashboardWidget{}
	for _, placement := range placements {
		byDashboard[placement.DashboardID] = append(byDashboard[placement.DashboardID], placement)
	}

	result := &gen.ListDashboardsResult{Dashboards: make([]*gen.Dashboard, 0, len(rows)), BuiltIn: builtInViews()}
	for _, row := range rows {
		result.Dashboards = append(result.Dashboards, mv.BuildDashboardView(row, byDashboard[row.ID]))
	}
	return result, nil
}

// GetDashboard returns one dashboard with its cards and saved filters.
func (s *Service) GetDashboard(ctx context.Context, payload *gen.GetDashboardPayload) (*gen.Dashboard, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid dashboard id")
	}
	queries := repo.New(s.db)

	row, err := queries.GetDashboard(ctx, repo.GetDashboardParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "dashboard not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get dashboard").LogError(ctx, s.logger)
	}
	return s.view(ctx, queries, row)
}

// CreateDashboard makes an empty dashboard.
func (s *Service) CreateDashboard(ctx context.Context, payload *gen.CreateDashboardPayload) (*gen.Dashboard, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "making a dashboard requires a user identity")
	}
	details, reason := checkDetails(payload.Name, payload.Description)
	if reason != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s", reason)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin dashboard creation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateDashboard(ctx, repo.CreateDashboardParams{
		ProjectID:       *authCtx.ProjectID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		CreatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		Name:            details.name,
		Description:     conv.PtrToPGTextEmpty(details.description),
		Filters:         []byte("{}"),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create dashboard").LogError(ctx, s.logger)
	}

	view := mv.BuildDashboardView(row, nil)
	if err := s.audit.LogDashboardCreate(ctx, dbtx, audit.LogDashboardCreateEvent{DashboardEventBase: s.auditBase(authCtx, row), Snapshot: view, DuplicatedFrom: nil, DuplicatedFromBuiltIn: ""}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit dashboard creation").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit dashboard creation").LogError(ctx, s.logger)
	}
	return view, nil
}

// UpdateDashboard renames a dashboard or changes its description.
func (s *Service) UpdateDashboard(ctx context.Context, payload *gen.UpdateDashboardPayload) (*gen.Dashboard, error) {
	details, reason := checkDetails(payload.Name, payload.Description)
	if reason != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s", reason)
	}
	return s.edit(ctx, payload.ID, "update", func(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, before repo.Dashboard) (repo.Dashboard, error) {
		row, err := queries.UpdateDashboard(ctx, repo.UpdateDashboardParams{
			Name:        details.name,
			Description: conv.PtrToPGTextEmpty(details.description),
			ProjectID:   *authCtx.ProjectID,
			ID:          before.ID,
		})
		if err != nil {
			return repo.Dashboard{}, fmt.Errorf("update dashboard: %w", err)
		}
		return row, nil
	})
}

// SaveDashboardLayout moves and resizes a dashboard's cards: each names an
// existing placement and where it now sits, and any placement not listed is
// removed. Adding a card is AddDashboardWidget's job, so a caller never has
// to match new ids in the response back to the cards it sent. Layout
// autosaves from the grid, so the last save wins.
func (s *Service) SaveDashboardLayout(ctx context.Context, payload *gen.SaveDashboardLayoutPayload) (*gen.Dashboard, error) {
	if len(payload.Placements) > maxCards {
		return nil, oops.E(oops.CodeBadRequest, nil, "a dashboard holds at most %d cards", maxCards)
	}
	return s.edit(ctx, payload.ID, "layout", func(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, dashboard repo.Dashboard) (repo.Dashboard, error) {
		existing, err := queries.ListPlacements(ctx, repo.ListPlacementsParams{ProjectID: *authCtx.ProjectID, DashboardID: dashboard.ID})
		if err != nil {
			return repo.Dashboard{}, fmt.Errorf("list dashboard cards: %w", err)
		}
		byID := make(map[uuid.UUID]repo.DashboardWidget, len(existing))
		for _, placement := range existing {
			byID[placement.ID] = placement
		}

		// Every card is checked before any is written, so a refused layout
		// changes nothing. A widget is read once however many cards show it:
		// for its chart type, which sets the card's minimum size, and to
		// refuse a widget of another project.
		type card struct {
			placed
			placementID uuid.UUID
		}
		cards := make([]card, 0, len(payload.Placements))
		widgetsByID := make(map[uuid.UUID]repo.Widget)
		for i, input := range payload.Placements {
			position := fmt.Sprintf("placements[%d]", i)
			if input == nil {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s: a placement is required", position)
			}
			if input.ID == "" {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s: a card needs its id; add a widget with addWidget", position)
			}
			id, err := uuid.Parse(input.ID)
			if err != nil {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, err, "%s: invalid placement id", position)
			}
			widgetID, err := uuid.Parse(input.WidgetID)
			if err != nil {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, err, "%s: invalid widget id", position)
			}
			current, ok := byID[id]
			if !ok {
				// The card went while this layout was being made: someone
				// took it off, or its widget was deleted. There is nothing
				// to move, and it is not kept; the rest of the layout still
				// lands rather than failing until a reload.
				continue
			}
			if current.WidgetID != widgetID {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s: card %s shows another widget; remove it and add the new one", position, id)
			}
			widget, ok := widgetsByID[widgetID]
			if !ok {
				loaded, err := queries.GetWidgetForPlacement(ctx, repo.GetWidgetForPlacementParams{ProjectID: *authCtx.ProjectID, ID: widgetID})
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s: widget %s is not in this project", position, widgetID)
					}
					return repo.Dashboard{}, fmt.Errorf("load widget for card: %w", err)
				}
				widget = loaded
				widgetsByID[widgetID] = loaded
			}
			if reason := checkPlacement(position, input, chartTypeOf(widget.Visualization)); reason != "" {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s", reason)
			}
			cards = append(cards, card{placed: placed{position: position, input: input}, placementID: id})
		}
		// Cards the layout does not name keep their place, so the named ones
		// must fit around them as well as around each other: a layout saved
		// from an older view cannot be dropped on top of a card added since.
		named := make(map[uuid.UUID]bool, len(cards))
		for _, c := range cards {
			named[c.placementID] = true
		}
		// The resting cards go first, so an overlap is reported against the
		// named card that would land on one.
		laid := make([]placed, 0, len(existing))
		for _, current := range existing {
			if named[current.ID] {
				continue
			}
			laid = append(laid, placed{
				position: fmt.Sprintf("card %s, not in this layout", current.ID),
				input:    &gen.PlacementInput{ID: current.ID.String(), WidgetID: current.WidgetID.String(), X: int(current.X), Y: int(current.Y), W: int(current.W), H: int(current.H)},
			})
		}
		for _, c := range cards {
			laid = append(laid, c.placed)
		}
		if reason := checkOverlaps(laid); reason != "" {
			return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s", reason)
		}

		// Cards the layout does not name stay where they are: a layout saved
		// from an older view cannot take off a card someone has just added.
		// Removing a card is its own call.
		for _, c := range cards {
			if _, err := queries.MovePlacement(ctx, repo.MovePlacementParams{
				X: int32(c.input.X), Y: int32(c.input.Y), W: int32(c.input.W), H: int32(c.input.H), //nolint:gosec // bounded by checkPlacement
				ProjectID: *authCtx.ProjectID, DashboardID: dashboard.ID, ID: c.placementID,
			}); err != nil {
				return repo.Dashboard{}, fmt.Errorf("move dashboard card: %w", err)
			}
		}
		return s.touch(ctx, authCtx, queries, dashboard.ID)
	})
}

// AddDashboardWidget places a saved widget on a dashboard as a new card at
// the bottom, sized for its chart type.
func (s *Service) AddDashboardWidget(ctx context.Context, payload *gen.AddDashboardWidgetPayload) (*gen.Dashboard, error) {
	widgetID, err := uuid.Parse(payload.WidgetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid widget id")
	}
	return s.edit(ctx, payload.ID, "add card", func(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, dashboard repo.Dashboard) (repo.Dashboard, error) {
		widget, err := queries.GetWidgetForPlacement(ctx, repo.GetWidgetForPlacementParams{ProjectID: *authCtx.ProjectID, ID: widgetID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return repo.Dashboard{}, oops.E(oops.CodeNotFound, err, "widget not found")
			}
			return repo.Dashboard{}, fmt.Errorf("load widget: %w", err)
		}
		existing, err := queries.ListPlacements(ctx, repo.ListPlacementsParams{ProjectID: *authCtx.ProjectID, DashboardID: dashboard.ID})
		if err != nil {
			return repo.Dashboard{}, fmt.Errorf("list dashboard cards: %w", err)
		}
		if len(existing) >= maxCards {
			return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "a dashboard holds at most %d cards", maxCards)
		}
		// The new card goes under everything already there.
		var bottom int32
		for _, placement := range existing {
			bottom = max(bottom, placement.Y+placement.H)
		}
		_, opening := cardSizes(chartTypeOf(widget.Visualization))
		if bottom > maxGridRow-int32(opening.h) { //nolint:gosec // a fixed small size
			return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "the dashboard has no room below row %d for another card", maxGridRow)
		}
		if _, err := queries.InsertPlacement(ctx, repo.InsertPlacementParams{
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
			DashboardID:    dashboard.ID,
			WidgetID:       widgetID,
			X:              0,
			Y:              bottom,
			W:              int32(opening.w), //nolint:gosec // a fixed small size
			H:              int32(opening.h), //nolint:gosec // a fixed small size
		}); err != nil {
			return repo.Dashboard{}, fmt.Errorf("add dashboard card: %w", err)
		}
		return s.touch(ctx, authCtx, queries, dashboard.ID)
	})
}

// RemoveDashboardWidget takes a card off a dashboard. The widget stays.
func (s *Service) RemoveDashboardWidget(ctx context.Context, payload *gen.RemoveDashboardWidgetPayload) (*gen.Dashboard, error) {
	placementID, err := uuid.Parse(payload.PlacementID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid placement id")
	}
	return s.edit(ctx, payload.ID, "remove card", func(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, dashboard repo.Dashboard) (repo.Dashboard, error) {
		if _, err := queries.DeletePlacement(ctx, repo.DeletePlacementParams{ProjectID: *authCtx.ProjectID, DashboardID: dashboard.ID, ID: placementID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return repo.Dashboard{}, oops.E(oops.CodeNotFound, err, "card not found")
			}
			return repo.Dashboard{}, fmt.Errorf("remove dashboard card: %w", err)
		}
		return s.touch(ctx, authCtx, queries, dashboard.ID)
	})
}

// SaveDashboardFilters stores the date range and filter values a dashboard
// opens on, for everyone.
func (s *Service) SaveDashboardFilters(ctx context.Context, payload *gen.SaveDashboardFiltersPayload) (*gen.Dashboard, error) {
	if reason := checkFilters(s.catalog, payload.Filters); reason != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s", reason)
	}
	encoded, err := mv.EncodeDashboardFilters(payload.Filters)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "filters are not encodable as JSON")
	}
	return s.edit(ctx, payload.ID, "filters", func(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, dashboard repo.Dashboard) (repo.Dashboard, error) {
		row, err := queries.UpdateDashboardFilters(ctx, repo.UpdateDashboardFiltersParams{Filters: encoded, ProjectID: *authCtx.ProjectID, ID: dashboard.ID})
		if err != nil {
			return repo.Dashboard{}, fmt.Errorf("save dashboard filters: %w", err)
		}
		return row, nil
	})
}

// DuplicateDashboard copies a dashboard into a new one the caller owns, and
// every widget on it into a new saved widget, so the copy is independent.
// Like every widget save the copies are validated, so a dashboard with a
// broken widget cannot be duplicated until the widget is fixed.
func (s *Service) DuplicateDashboard(ctx context.Context, payload *gen.DuplicateDashboardPayload) (*gen.Dashboard, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "making a dashboard requires a user identity")
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid dashboard id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin dashboard duplication").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	// Held for the copy, so a layout save on the source waits and the cards
	// and widgets read here agree with each other.
	source, err := queries.GetDashboardForUpdate(ctx, repo.GetDashboardForUpdateParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "dashboard not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load dashboard").LogError(ctx, s.logger)
	}
	sourceWidgets, err := queries.ListWidgetsForDashboard(ctx, repo.ListWidgetsForDashboardParams{ProjectID: *authCtx.ProjectID, DashboardID: source.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load dashboard widgets").LogError(ctx, s.logger)
	}
	placements, err := queries.ListPlacements(ctx, repo.ListPlacementsParams{ProjectID: *authCtx.ProjectID, DashboardID: source.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load dashboard cards").LogError(ctx, s.logger)
	}

	sourceURN := urn.NewDashboard(source.ID)
	from := copySource{
		name:        source.Name,
		description: source.Description,
		filters:     source.Filters,
		widgets:     make([]copyWidget, 0, len(sourceWidgets)),
		placements:  make([]copyPlacement, 0, len(placements)),
		from:        &sourceURN,
		fromBuiltIn: "",
	}
	for _, widget := range sourceWidgets {
		widgetURN := urn.NewWidget(widget.ID)
		from.widgets = append(from.widgets, copyWidget{
			key:           widget.ID.String(),
			name:          widget.Name,
			description:   widget.Description,
			dataset:       widget.Dataset,
			query:         widget.Query,
			visualization: widget.Visualization,
			from:          &widgetURN,
		})
	}
	for _, placement := range placements {
		from.placements = append(from.placements, copyPlacement{key: placement.WidgetID.String(), x: placement.X, y: placement.Y, w: placement.W, h: placement.H})
	}

	view, err := s.copyInto(ctx, dbtx, authCtx, from)
	if err != nil {
		return nil, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit dashboard duplication").LogError(ctx, s.logger)
	}
	return view, nil
}

// DuplicateBuiltInDashboard copies a built-in dashboard into a new one the
// caller owns, with a new saved widget per card, so the copy can be changed
// while the built-in stays as it is.
func (s *Service) DuplicateBuiltInDashboard(ctx context.Context, payload *gen.DuplicateBuiltInDashboardPayload) (*gen.Dashboard, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "making a dashboard requires a user identity")
	}
	page, ok := builtInBySlug(payload.Slug)
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "built-in dashboard not found")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin dashboard duplication").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// A built-in saves no filters: the copy opens on the defaults, as a new
	// dashboard does. Each card becomes its own widget, keyed by its place
	// in the layout.
	from := copySource{
		name:        page.Name,
		description: conv.ToPGTextEmpty(page.Description),
		filters:     []byte("{}"),
		widgets:     make([]copyWidget, 0, len(page.Cards)),
		placements:  make([]copyPlacement, 0, len(page.Cards)),
		from:        nil,
		fromBuiltIn: page.Slug,
	}
	for i, card := range page.Cards {
		key := strconv.Itoa(i)
		from.widgets = append(from.widgets, copyWidget{
			key:           key,
			name:          card.Name,
			description:   conv.ToPGTextEmpty(card.Description),
			dataset:       card.Dataset,
			query:         card.Query,
			visualization: card.Visualization,
			from:          nil,
		})
		from.placements = append(from.placements, copyPlacement{
			key: key,
			x:   int32(card.X), y: int32(card.Y), w: int32(card.W), h: int32(card.H), //nolint:gosec // bounded by the registry test
		})
	}

	view, err := s.copyInto(ctx, dbtx, authCtx, from)
	if err != nil {
		return nil, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit dashboard duplication").LogError(ctx, s.logger)
	}
	return view, nil
}

// copySource is what a dashboard copy is made from: a project dashboard's
// rows, or a built-in's cards. A placement names its widget by key, so one
// widget shown on several cards is copied once.
type copySource struct {
	name        string
	description pgtype.Text
	filters     []byte
	widgets     []copyWidget
	placements  []copyPlacement
	// Where the copy came from, for the audit trail: the dashboard, or the
	// built-in's slug.
	from        *urn.Dashboard
	fromBuiltIn string
}

type copyWidget struct {
	key           string
	name          string
	description   pgtype.Text
	dataset       string
	query         []byte
	visualization []byte
	// The saved widget this is a copy of, when it is one; a built-in card is
	// not a widget.
	from *urn.Widget
}

type copyPlacement struct {
	key        string
	x, y, w, h int32
}

// copyInto makes the caller's copy of a source inside the given transaction:
// each widget copied once, however many cards show it, then the dashboard
// and its cards, every step audited. The caller commits. A widget the
// catalog has broken is refused by name, as duplicating it on its own would
// be.
func (s *Service) copyInto(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, source copySource) (*gen.Dashboard, error) {
	queries := repo.New(dbtx)
	widgetQueries := widgetsrepo.New(dbtx)
	now := s.now()
	copies := make(map[string]uuid.UUID, len(source.widgets))
	for _, widget := range source.widgets {
		reason, err := widgets.Validate(s.catalog, widget.dataset, widget.query, widget.visualization, now)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "validate widget").LogError(ctx, s.logger)
		}
		if reason != "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "widget %q cannot be copied until it is fixed: %s", widget.name, reason)
		}
		copied, err := widgetQueries.CreateWidget(ctx, widgetsrepo.CreateWidgetParams{
			ProjectID:       *authCtx.ProjectID,
			OrganizationID:  authCtx.ActiveOrganizationID,
			CreatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
			Name:            copyName(widget.name),
			Description:     widget.description,
			Dataset:         widget.dataset,
			Query:           widget.query,
			Visualization:   widget.visualization,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "copy widget").LogError(ctx, s.logger)
		}
		if err := s.audit.LogWidgetCreate(ctx, dbtx, audit.LogWidgetCreateEvent{
			WidgetEventBase: audit.WidgetEventBase{
				OrganizationID:   authCtx.ActiveOrganizationID,
				ProjectID:        copied.ProjectID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
				ActorDisplayName: authCtx.Email,
				WidgetURN:        urn.NewWidget(copied.ID),
				Name:             copied.Name,
			},
			Snapshot:       mv.BuildWidgetView(copied, "", nil),
			DuplicatedFrom: widget.from,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit widget copy").LogError(ctx, s.logger)
		}
		copies[widget.key] = copied.ID
	}

	row, err := queries.CreateDashboard(ctx, repo.CreateDashboardParams{
		ProjectID:       *authCtx.ProjectID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		CreatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		Name:            copyName(source.name),
		Description:     source.description,
		Filters:         source.filters,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create dashboard copy").LogError(ctx, s.logger)
	}
	for _, placement := range source.placements {
		if _, err := queries.InsertPlacement(ctx, repo.InsertPlacementParams{
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
			DashboardID:    row.ID,
			WidgetID:       copies[placement.key],
			X:              placement.x,
			Y:              placement.y,
			W:              placement.w,
			H:              placement.h,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "copy dashboard card").LogError(ctx, s.logger)
		}
	}

	view, err := s.view(ctx, queries, row)
	if err != nil {
		return nil, err
	}
	if err := s.audit.LogDashboardCreate(ctx, dbtx, audit.LogDashboardCreateEvent{DashboardEventBase: s.auditBase(authCtx, row), Snapshot: view, DuplicatedFrom: source.from, DuplicatedFromBuiltIn: source.fromBuiltIn}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit dashboard copy").LogError(ctx, s.logger)
	}
	return view, nil
}

// DeleteDashboard soft-deletes a dashboard. Its cards go with it; its
// widgets stay saved.
func (s *Service) DeleteDashboard(ctx context.Context, payload *gen.DeleteDashboardPayload) error {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid dashboard id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin dashboard deletion").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	existing, err := queries.GetDashboardForUpdate(ctx, repo.GetDashboardForUpdateParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "dashboard not found")
		}
		return oops.E(oops.CodeUnexpected, err, "load dashboard").LogError(ctx, s.logger)
	}
	if err := s.requireOwnerOrWrite(ctx, authCtx, existing); err != nil {
		return err
	}

	row, err := queries.DeleteDashboard(ctx, repo.DeleteDashboardParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete dashboard").LogError(ctx, s.logger)
	}
	if err := s.audit.LogDashboardDelete(ctx, dbtx, audit.LogDashboardDeleteEvent{DashboardEventBase: s.auditBase(authCtx, row)}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit dashboard deletion").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit dashboard deletion").LogError(ctx, s.logger)
	}
	return nil
}

// change is one edit to a dashboard, run inside edit's transaction with the
// dashboard row locked and the caller's right to change it checked. It
// returns the dashboard row as it stands afterwards. An oops error is
// returned to the caller as is; any other error is a server error.
type change func(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, dashboard repo.Dashboard) (repo.Dashboard, error)

// edit is the shape of every change to a dashboard that keeps it: lock the
// row, check who may change it, apply the change, audit before and after,
// commit. what names the change in error messages.
func (s *Service) edit(ctx context.Context, rawID string, what string, apply change) (*gen.Dashboard, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid dashboard id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin dashboard %s", what).LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetDashboardForUpdate(ctx, repo.GetDashboardForUpdateParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "dashboard not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load dashboard").LogError(ctx, s.logger)
	}
	if err := s.requireOwnerOrWrite(ctx, authCtx, before); err != nil {
		return nil, err
	}
	beforeView, err := s.view(ctx, queries, before)
	if err != nil {
		return nil, err
	}

	after, err := apply(ctx, authCtx, queries, before)
	if err != nil {
		if _, ok := errors.AsType[*oops.ShareableError](err); ok {
			return nil, err
		}
		return nil, oops.E(oops.CodeUnexpected, err, "dashboard %s", what).LogError(ctx, s.logger)
	}
	afterView, err := s.view(ctx, queries, after)
	if err != nil {
		return nil, err
	}

	// A change to the cards is audited as a layout change, with the cards
	// alone: the grid autosaves every drag, so a full dashboard snapshot per
	// move would swamp the log.
	var audited error
	if what == "layout" || what == "add card" || what == "remove card" {
		audited = s.audit.LogDashboardLayout(ctx, dbtx, audit.LogDashboardLayoutEvent{DashboardEventBase: s.auditBase(authCtx, after), Before: beforeView.Widgets, After: afterView.Widgets})
	} else {
		audited = s.audit.LogDashboardUpdate(ctx, dbtx, audit.LogDashboardUpdateEvent{DashboardEventBase: s.auditBase(authCtx, after), Before: beforeView, After: afterView})
	}
	if audited != nil {
		return nil, oops.E(oops.CodeUnexpected, audited, "audit dashboard %s", what).LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit dashboard %s", what).LogError(ctx, s.logger)
	}
	return afterView, nil
}

// touch marks a dashboard changed after an edit to its cards, so it moves
// up the list, and returns the row.
func (s *Service) touch(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, id uuid.UUID) (repo.Dashboard, error) {
	row, err := queries.TouchDashboard(ctx, repo.TouchDashboardParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		return repo.Dashboard{}, fmt.Errorf("touch dashboard: %w", err)
	}
	return row, nil
}

// view renders a dashboard with its cards.
func (s *Service) view(ctx context.Context, queries *repo.Queries, row repo.Dashboard) (*gen.Dashboard, error) {
	placements, err := queries.ListPlacements(ctx, repo.ListPlacementsParams{ProjectID: row.ProjectID, DashboardID: row.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list dashboard cards").LogError(ctx, s.logger)
	}
	return mv.BuildDashboardView(row, placements), nil
}

func (s *Service) auditBase(authCtx *contextvalues.AuthContext, row repo.Dashboard) audit.DashboardEventBase {
	return audit.DashboardEventBase{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        row.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		DashboardURN:     urn.NewDashboard(row.ID),
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
