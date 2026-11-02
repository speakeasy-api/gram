package widgets

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	dashboardsrepo "github.com/speakeasy-api/gram/server/internal/dashboards/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

// maxDescriptionLength matches the widgets RPC's limit on a description.
const maxDescriptionLength = 2000

// ErrVersionChanged marks a write that named the version it expected and
// found the widget changed since. It is wrapped in a conflict error.
var ErrVersionChanged = errors.New("widget changed since it was read")

// ErrDatasetRestricted marks a save over a dataset the caller may not read,
// such as one only organization administrators may read. It is wrapped in a
// forbidden error.
var ErrDatasetRestricted = errors.New("dataset is restricted")

// Input is a widget as a caller wants it saved.
type Input struct {
	// Name is the display name, 1 to 200 characters.
	Name string `json:"name"`
	// Description says what the widget is for, at most 2000 characters.
	Description *string `json:"description"`
	// Dataset is the catalog dataset the widget asks.
	Dataset string `json:"dataset"`
	// Query is the question, validated against the catalog.
	Query map[string]any `json:"query"`
	// Visualization is how the question is drawn, checked against the query.
	Visualization map[string]any `json:"visualization"`
}

// Snapshot is a widget as read or written, with the version a later write
// can expect.
type Snapshot struct {
	// Widget is the widget as the RPC shows it.
	Widget *gen.Widget

	// Version is the widget's updated_at at full precision. Every write moves
	// it, so a caller that read one version can refuse to overwrite another.
	Version string
}

// Version is a stored widget's version: its updated_at at the database's
// full precision, which the RFC 3339 updated_at in the view rounds to the
// second.
func Version(row repo.Widget) string {
	return row.UpdatedAt.Time.UTC().Format(time.RFC3339Nano)
}

// Caller is a request admitted to its project's widgets. Writes take one so
// authorization runs before their transaction opens: a check logs its
// challenge through the pool, which must not wait on a connection the
// transaction holds.
type Caller struct {
	// auth is the request's auth context, with its project selected.
	auth *contextvalues.AuthContext
}

// Change is a caller admitted to update or delete one widget.
type Change struct {
	// caller is who is making the change.
	caller Caller
	// widgetID is the widget the caller may change.
	widgetID uuid.UUID
}

// Authorize admits the request's caller to its project's widgets.
func (s *Service) Authorize(ctx context.Context) (Caller, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return Caller{}, err
	}
	return Caller{auth: authCtx}, nil
}

// AuthorizeChange admits the request's caller to update or delete one widget:
// its creator, or anyone with project write access. A widget's creator never
// changes, so the check holds for the transaction that follows; a widget that
// does not exist is left for that transaction to refuse.
func (s *Service) AuthorizeChange(ctx context.Context, id string) (Change, error) {
	caller, err := s.Authorize(ctx)
	if err != nil {
		return Change{}, err
	}
	widgetID, err := parseID(id)
	if err != nil {
		return Change{}, err
	}
	row, err := repo.New(s.db).GetWidget(ctx, repo.GetWidgetParams{ProjectID: *caller.auth.ProjectID, ID: widgetID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Change{}, oops.E(oops.CodeUnexpected, err, "get widget").LogError(ctx, s.logger)
	default:
		if err := s.requireOwnerOrWrite(ctx, caller.auth, conv.FromPGText[string](row.CreatedByUserID)); err != nil {
			return Change{}, err
		}
	}
	return Change{caller: caller, widgetID: widgetID}, nil
}

func (c Caller) admitted() (*contextvalues.AuthContext, error) {
	if c.auth == nil || c.auth.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return c.auth, nil
}

// Read returns one widget of the request's project, validated as it is read,
// with its dashboards and version.
func (s *Service) Read(ctx context.Context, id string) (Snapshot, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return Snapshot{}, err
	}
	return s.read(ctx, authCtx, id)
}

func (s *Service) read(ctx context.Context, authCtx *contextvalues.AuthContext, id string) (Snapshot, error) {
	widgetID, err := parseID(id)
	if err != nil {
		return Snapshot{}, err
	}

	queries := repo.New(s.db)
	row, err := queries.GetWidget(ctx, repo.GetWidgetParams{ProjectID: *authCtx.ProjectID, ID: widgetID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return Snapshot{}, oops.E(oops.CodeUnexpected, err, "get widget").LogError(ctx, s.logger)
	}
	dashboards, err := s.usage(ctx, queries, *authCtx.ProjectID, widgetID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Widget: s.view(ctx, row, s.now(), dashboards), Version: Version(row)}, nil
}

// ReadForChange reads a widget and refuses, as AuthorizeChange does,
// a caller who may not change it, so a preview never shows a change
// that would be refused.
func (s *Service) ReadForChange(ctx context.Context, id string) (Snapshot, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := s.read(ctx, authCtx, id)
	if err != nil {
		return Snapshot{}, err
	}
	if err := s.requireOwnerOrWrite(ctx, authCtx, snapshot.Widget.CreatedByUserID); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// ReadAll lists the request project's widgets, most recently updated first,
// each validated as it is read, with its dashboards and version.
func (s *Service) ReadAll(ctx context.Context) ([]Snapshot, error) {
	return s.list(ctx, pgtype.Int4{Int32: 0, Valid: false})
}

// ReadRecent is ReadAll bounded to limit widgets, reading and validating no
// more than that.
func (s *Service) ReadRecent(ctx context.Context, limit int32) ([]Snapshot, error) {
	return s.list(ctx, pgtype.Int4{Int32: limit, Valid: true})
}

func (s *Service) list(ctx context.Context, limit pgtype.Int4) ([]Snapshot, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}

	queries := repo.New(s.db)
	rows, err := queries.ListWidgets(ctx, repo.ListWidgetsParams{ProjectID: *authCtx.ProjectID, RowLimit: limit})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list widgets").LogError(ctx, s.logger)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	placed, err := queries.ListDashboardsForWidgets(ctx, repo.ListDashboardsForWidgetsParams{ProjectID: *authCtx.ProjectID, WidgetIds: ids})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list widget dashboards").LogError(ctx, s.logger)
	}
	byWidget := map[uuid.UUID][]*gen.WidgetDashboard{}
	for _, row := range placed {
		byWidget[row.WidgetID] = append(byWidget[row.WidgetID], &gen.WidgetDashboard{ID: row.DashboardID.String(), Name: row.DashboardName})
	}

	now := s.now()
	out := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, Snapshot{Widget: s.view(ctx, row, now, byWidget[row.ID]), Version: Version(row)})
	}
	return out, nil
}

// Check validates a widget as a save would, without writing it: the query
// against the catalog and the visualization against the query.
func (s *Service) Check(ctx context.Context, input Input) error {
	if _, err := s.authorize(ctx, authz.ScopeProjectRead); err != nil {
		return err
	}
	_, _, err := s.checkInput(ctx, input)
	return err
}

// CreateInTx saves a new widget and audits it inside the caller's
// transaction, after validating it as CreateWidget does.
func (s *Service) CreateInTx(ctx context.Context, tx pgx.Tx, caller Caller, input Input) (Snapshot, error) {
	authCtx, err := caller.admitted()
	if err != nil {
		return Snapshot{}, err
	}

	query, visualization, err := s.checkInput(ctx, input)
	if err != nil {
		return Snapshot{}, err
	}

	return s.insert(ctx, tx, authCtx, repo.CreateWidgetParams{
		ProjectID:       *authCtx.ProjectID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		CreatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		Name:            input.Name,
		Description:     conv.PtrToPGTextEmpty(input.Description),
		Dataset:         input.Dataset,
		Query:           query,
		Visualization:   visualization,
	}, nil)
}

// DuplicateInTx copies a widget into a new one the caller owns, inside the
// caller's transaction. A non-empty expectedVersion refuses the copy when the
// source changed since it was read.
func (s *Service) DuplicateInTx(ctx context.Context, tx pgx.Tx, caller Caller, id string, expectedVersion string) (Snapshot, error) {
	authCtx, err := caller.admitted()
	if err != nil {
		return Snapshot{}, err
	}
	widgetID, err := parseID(id)
	if err != nil {
		return Snapshot{}, err
	}

	source, err := s.sourceWidget(ctx, repo.New(tx), *authCtx.ProjectID, widgetID, expectedVersion)
	if err != nil {
		return Snapshot{}, err
	}
	if ds, ok := s.catalog.Dataset(source.Dataset); ok {
		if err := s.requireDataset(ctx, ds); err != nil {
			return Snapshot{}, err
		}
	}
	reason, err := s.problem(ctx, source.Dataset, source.Query, source.Visualization)
	if err != nil {
		return Snapshot{}, err
	}
	if reason != "" {
		return Snapshot{}, oops.E(oops.CodeBadRequest, nil, "widget cannot be duplicated until it is fixed: %s", reason)
	}

	sourceURN := urn.NewWidget(source.ID)
	return s.insert(ctx, tx, authCtx, repo.CreateWidgetParams{
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

func (s *Service) insert(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, params repo.CreateWidgetParams, duplicatedFrom *urn.Widget) (Snapshot, error) {
	if authCtx.UserID == "" {
		return Snapshot{}, oops.E(oops.CodeUnauthorized, nil, "saving a widget requires a user identity")
	}

	row, err := repo.New(tx).CreateWidget(ctx, params)
	if err != nil {
		return Snapshot{}, oops.E(oops.CodeUnexpected, err, "create widget").LogError(ctx, s.logger)
	}

	view := mv.BuildWidgetView(row, "", nil)
	if err := s.audit.LogWidgetCreate(ctx, tx, audit.LogWidgetCreateEvent{WidgetEventBase: s.auditBase(authCtx, row), Snapshot: view, DuplicatedFrom: duplicatedFrom}); err != nil {
		return Snapshot{}, oops.E(oops.CodeUnexpected, err, "audit widget creation").LogError(ctx, s.logger)
	}
	return Snapshot{Widget: view, Version: Version(row)}, nil
}

// UpdateInTx replaces the widget a change admits inside the caller's
// transaction. A non-empty expectedVersion refuses the update when the widget
// changed since it was read.
func (s *Service) UpdateInTx(ctx context.Context, tx pgx.Tx, change Change, input Input, expectedVersion string) (Snapshot, error) {
	authCtx, err := change.caller.admitted()
	if err != nil {
		return Snapshot{}, err
	}
	widgetID := change.widgetID

	query, visualization, err := s.checkInput(ctx, input)
	if err != nil {
		return Snapshot{}, err
	}

	queries := repo.New(tx)
	before, err := s.lockWidget(ctx, queries, *authCtx.ProjectID, widgetID, expectedVersion)
	if err != nil {
		return Snapshot{}, err
	}

	row, err := queries.UpdateWidget(ctx, repo.UpdateWidgetParams{
		Name:          input.Name,
		Description:   conv.PtrToPGTextEmpty(input.Description),
		Dataset:       input.Dataset,
		Query:         query,
		Visualization: visualization,
		ProjectID:     *authCtx.ProjectID,
		ID:            widgetID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return Snapshot{}, oops.E(oops.CodeUnexpected, err, "update widget").LogError(ctx, s.logger)
	}

	dashboards, err := s.usage(ctx, queries, *authCtx.ProjectID, widgetID)
	if err != nil {
		return Snapshot{}, err
	}
	view := mv.BuildWidgetView(row, "", dashboards)
	if err := s.audit.LogWidgetUpdate(ctx, tx, audit.LogWidgetUpdateEvent{WidgetEventBase: s.auditBase(authCtx, row), Before: s.view(ctx, before, s.now(), dashboards), After: view}); err != nil {
		return Snapshot{}, oops.E(oops.CodeUnexpected, err, "audit widget update").LogError(ctx, s.logger)
	}
	return Snapshot{Widget: view, Version: Version(row)}, nil
}

// DeleteInTx soft-deletes the widget a change admits inside the caller's
// transaction and returns the dashboards it was removed from. A non-empty
// expectedVersion refuses the delete when the widget changed since it was
// read.
func (s *Service) DeleteInTx(ctx context.Context, tx pgx.Tx, change Change, expectedVersion string) ([]*gen.WidgetDashboard, error) {
	authCtx, err := change.caller.admitted()
	if err != nil {
		return nil, err
	}
	widgetID := change.widgetID

	queries := repo.New(tx)
	dashboards := dashboardsrepo.New(tx)
	// A layout change locks a dashboard and then share-locks its widgets; this
	// locks the widget and then touches its dashboards. The project's
	// dashboard lock, taken first by both, keeps the two from crossing.
	if err := dashboards.LockProjectDashboards(ctx, *authCtx.ProjectID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock project dashboards").LogError(ctx, s.logger)
	}
	if _, err := s.lockWidget(ctx, queries, *authCtx.ProjectID, widgetID, expectedVersion); err != nil {
		return nil, err
	}

	// Each affected dashboard is touched and audited as a layout change, so its
	// history says why a card went.
	affected, err := queries.ListDashboardsForWidget(ctx, repo.ListDashboardsForWidgetParams{ProjectID: *authCtx.ProjectID, WidgetID: widgetID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list widget's dashboards").LogError(ctx, s.logger)
	}
	before := make(map[uuid.UUID][]dashboardsrepo.DashboardWidget, len(affected))
	for _, dashboard := range affected {
		cards, err := dashboards.ListPlacements(ctx, dashboardsrepo.ListPlacementsParams{ProjectID: *authCtx.ProjectID, DashboardID: dashboard.DashboardID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list dashboard cards").LogError(ctx, s.logger)
		}
		before[dashboard.DashboardID] = cards
	}
	if err := queries.DeleteWidgetPlacements(ctx, repo.DeleteWidgetPlacementsParams{ProjectID: *authCtx.ProjectID, WidgetID: widgetID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "remove widget from dashboards").LogError(ctx, s.logger)
	}
	removedFrom := make([]urn.Dashboard, 0, len(affected))
	for _, dashboard := range affected {
		after, err := dashboards.ListPlacements(ctx, dashboardsrepo.ListPlacementsParams{ProjectID: *authCtx.ProjectID, DashboardID: dashboard.DashboardID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list dashboard cards").LogError(ctx, s.logger)
		}
		if _, err := dashboards.TouchDashboard(ctx, dashboardsrepo.TouchDashboardParams{ProjectID: *authCtx.ProjectID, ID: dashboard.DashboardID}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "touch dashboard").LogError(ctx, s.logger)
		}
		dashboardURN := urn.NewDashboard(dashboard.DashboardID)
		if err := s.audit.LogDashboardLayout(ctx, tx, audit.LogDashboardLayoutEvent{
			DashboardEventBase: audit.DashboardEventBase{
				OrganizationID:   authCtx.ActiveOrganizationID,
				ProjectID:        *authCtx.ProjectID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
				ActorDisplayName: authCtx.Email,
				DashboardURN:     dashboardURN,
				Name:             dashboard.DashboardName,
			},
			Before: mv.BuildDashboardPlacements(before[dashboard.DashboardID]),
			After:  mv.BuildDashboardPlacements(after),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit dashboard layout").LogError(ctx, s.logger)
		}
		removedFrom = append(removedFrom, dashboardURN)
	}
	row, err := queries.DeleteWidget(ctx, repo.DeleteWidgetParams{ProjectID: *authCtx.ProjectID, ID: widgetID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "delete widget").LogError(ctx, s.logger)
	}
	if err := s.audit.LogWidgetDelete(ctx, tx, audit.LogWidgetDeleteEvent{WidgetEventBase: s.auditBase(authCtx, row), RemovedFrom: removedFrom}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit widget deletion").LogError(ctx, s.logger)
	}
	return dashboardsOf(affected), nil
}

// checkInput validates a widget and encodes its query and visualization for
// storage. It repeats the RPC's name and description limits for callers that
// bring their own input.
func (s *Service) checkInput(ctx context.Context, input Input) ([]byte, []byte, error) {
	if length := utf8.RuneCountInString(input.Name); length == 0 || length > maxNameLength {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "invalid_name: a widget's name must be 1 to %d characters", maxNameLength)
	}
	if input.Description != nil && utf8.RuneCountInString(*input.Description) > maxDescriptionLength {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "invalid_description: a widget's description must be at most %d characters", maxDescriptionLength)
	}
	return s.checkWidget(ctx, input.Dataset, input.Query, input.Visualization)
}

// lockWidget loads a widget for update, holding its row lock for the rest of
// the transaction, and refuses it as a conflict when expectedVersion is set
// and the widget has moved on from it.
func (s *Service) lockWidget(ctx context.Context, queries *repo.Queries, projectID, widgetID uuid.UUID, expectedVersion string) (repo.Widget, error) {
	row, err := queries.GetWidgetForUpdate(ctx, repo.GetWidgetForUpdateParams{ProjectID: projectID, ID: widgetID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.Widget{}, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return repo.Widget{}, oops.E(oops.CodeUnexpected, err, "load widget").LogError(ctx, s.logger)
	}
	if expectedVersion != "" && Version(row) != expectedVersion {
		return repo.Widget{}, oops.E(oops.CodeConflict, ErrVersionChanged, "widget changed since it was read")
	}
	return row, nil
}

// sourceWidget loads the widget a duplicate copies. With an expected version
// the source is locked, so it cannot change between the version check and
// the copy; without one it is read as it stands, which is all a copy needs.
func (s *Service) sourceWidget(ctx context.Context, queries *repo.Queries, projectID, widgetID uuid.UUID, expectedVersion string) (repo.Widget, error) {
	if expectedVersion != "" {
		return s.lockWidget(ctx, queries, projectID, widgetID, expectedVersion)
	}
	row, err := queries.GetWidget(ctx, repo.GetWidgetParams{ProjectID: projectID, ID: widgetID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.Widget{}, oops.E(oops.CodeNotFound, err, "widget not found")
		}
		return repo.Widget{}, oops.E(oops.CodeUnexpected, err, "load widget").LogError(ctx, s.logger)
	}
	return row, nil
}

func inTx[T any](ctx context.Context, s *Service, label string, write func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "begin %s", label).LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	out, err := write(dbtx)
	if err != nil {
		return zero, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "commit %s", label).LogError(ctx, s.logger)
	}
	return out, nil
}

func parseID(id string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid widget id")
	}
	return parsed, nil
}
