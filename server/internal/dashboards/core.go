package dashboards

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dashboards/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ErrStaleVersion marks a write refused because the dashboard changed after
// the version the caller read. It is wrapped in an oops conflict.
var ErrStaleVersion = errors.New("dashboard changed since it was read")

// MaxLayoutOperations bounds one EditDashboardLayoutInTx call. A dashboard
// holds at most maxCards cards, so moving every card of a full dashboard
// fits in one call.
const MaxLayoutOperations = maxCards

// Version is a dashboard's version: its last change time to the
// microsecond Postgres stores, which every write to the dashboard or its
// cards moves.
func Version(updatedAt pgtype.Timestamptz) string {
	return updatedAt.Time.UTC().Format(time.RFC3339Nano)
}

// checkVersion refuses a write whose expected version is not the locked
// row's. An empty expected version is the dashboard API's last save wins.
func checkVersion(row repo.Dashboard, expected string) error {
	if expected == "" || expected == Version(row.UpdatedAt) {
		return nil
	}
	return oops.E(oops.CodeConflict, ErrStaleVersion, "the dashboard changed since it was read; read it again")
}

// CardWidget is the saved widget behind a card, as a reader names it.
type CardWidget struct {
	// Name is the widget's display name.
	Name string

	// Dataset is the catalog dataset the widget asks.
	Dataset string

	// ChartType is how the widget draws, lowercased, or "" when unreadable.
	ChartType string
}

// Detail is one dashboard with its version and the widgets its cards show.
type Detail struct {
	// Dashboard is the dashboard with its cards and saved filters.
	Dashboard *gen.Dashboard

	// Version is the value a versioned write expects.
	Version string

	// Widgets are the widgets the cards show, by widget id.
	Widgets map[string]CardWidget
}

// ReadDashboard reads one project dashboard with its version and the
// widgets its cards show. The caller needs project:read.
func (s *Service) ReadDashboard(ctx context.Context, rawID string) (Detail, error) {
	var zero Detail
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return zero, err
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return zero, oops.E(oops.CodeBadRequest, err, "invalid dashboard id")
	}
	queries := repo.New(s.db)

	row, err := queries.GetDashboard(ctx, repo.GetDashboardParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, oops.E(oops.CodeNotFound, err, "dashboard not found")
		}
		return zero, oops.E(oops.CodeUnexpected, err, "get dashboard").LogError(ctx, s.logger)
	}
	view, err := s.view(ctx, queries, row)
	if err != nil {
		return zero, err
	}
	widgets, err := s.cardWidgets(ctx, queries, *authCtx.ProjectID, row.ID)
	if err != nil {
		return zero, err
	}
	return Detail{Dashboard: view, Version: Version(row.UpdatedAt), Widgets: widgets}, nil
}

// CardWidgetsInTx reads the widgets a dashboard's cards show inside the
// caller's transaction, so it sees what a write there just placed. The caller
// needs project:read.
func (s *Service) CardWidgetsInTx(ctx context.Context, dbtx pgx.Tx, rawID string) (map[string]CardWidget, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid dashboard id")
	}
	return s.cardWidgets(ctx, repo.New(dbtx), *authCtx.ProjectID, id)
}

func (s *Service) cardWidgets(ctx context.Context, queries *repo.Queries, projectID, dashboardID uuid.UUID) (map[string]CardWidget, error) {
	rows, err := queries.ListWidgetsForDashboard(ctx, repo.ListWidgetsForDashboardParams{ProjectID: projectID, DashboardID: dashboardID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load dashboard widgets").LogError(ctx, s.logger)
	}
	widgets := make(map[string]CardWidget, len(rows))
	for _, widget := range rows {
		widgets[widget.ID.String()] = CardWidget{Name: widget.Name, Dataset: widget.Dataset, ChartType: chartTypeOf(widget.Visualization)}
	}
	return widgets, nil
}

// BuiltInDashboard returns the built-in dashboard with this slug, whoever
// asks. A surface that shows it reads it through Service.ReadBuiltInDashboard,
// which holds the caller to the datasets its cards ask.
func BuiltInDashboard(slug string) (*gen.BuiltInDashboard, bool) {
	page, ok := builtInBySlug(slug)
	if !ok {
		return nil, false
	}
	return page.view(), true
}

// BuiltInChartType reads the chart type a built-in card draws, lowercased.
func BuiltInChartType(card *gen.BuiltInCard) string {
	if card == nil {
		return ""
	}
	chartType, _ := card.Visualization["type"].(string)
	return strings.ToLower(chartType)
}

// LayoutOperationKind is one kind of change to a dashboard's cards.
type LayoutOperationKind string

const (
	// LayoutAddCard places a saved widget of the project as a new card.
	LayoutAddCard LayoutOperationKind = "add_card"

	// LayoutMoveCard moves or resizes a card already on the dashboard.
	LayoutMoveCard LayoutOperationKind = "move_card"

	// LayoutRemoveCard takes a card off the dashboard; its widget stays.
	LayoutRemoveCard LayoutOperationKind = "remove_card"
)

// LayoutOperation is one change in an EditDashboardLayoutInTx call.
type LayoutOperation struct {
	// Kind says which change this is.
	Kind LayoutOperationKind

	// WidgetID is the saved widget an added card shows.
	WidgetID string

	// PlacementID is the card a move or removal names.
	PlacementID string

	// X and Y place the card; an added card defaults to the left edge
	// below every card laid out before it.
	X, Y *int

	// W and H size the card; an added card defaults to its chart type's
	// opening size.
	W, H *int
}

// EditDashboardLayoutInTx applies card changes in order inside the caller's
// transaction, under the grid's rules. The final layout is checked as a whole
// before anything is written, so cards can trade places in one call and a
// refused list changes nothing. A non-empty expectedVersion refuses the edit
// with ErrStaleVersion when the dashboard has changed since that version.
func (s *Service) EditDashboardLayoutInTx(ctx context.Context, dbtx pgx.Tx, id, expectedVersion string, operations []LayoutOperation) (*gen.Dashboard, error) {
	switch {
	case len(operations) == 0:
		return nil, oops.E(oops.CodeBadRequest, nil, "a layout edit needs at least one operation")
	case len(operations) > MaxLayoutOperations:
		return nil, oops.E(oops.CodeBadRequest, nil, "a layout edit holds at most %d operations", MaxLayoutOperations)
	}
	return s.editInTx(ctx, dbtx, id, expectedVersion, editLayout, func(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, dashboard repo.Dashboard) (repo.Dashboard, error) {
		return s.applyLayoutOperations(ctx, authCtx, queries, dashboard, operations)
	})
}

// layoutCard is a card as the operations leave it. A new card has no
// placement id until it is written.
type layoutCard struct {
	placementID uuid.UUID
	widgetID    uuid.UUID
	placed      placed
	changed     bool
}

func (s *Service) applyLayoutOperations(ctx context.Context, authCtx *contextvalues.AuthContext, queries *repo.Queries, dashboard repo.Dashboard, operations []LayoutOperation) (repo.Dashboard, error) {
	existing, err := queries.ListPlacements(ctx, repo.ListPlacementsParams{ProjectID: *authCtx.ProjectID, DashboardID: dashboard.ID})
	if err != nil {
		return repo.Dashboard{}, fmt.Errorf("list dashboard cards: %w", err)
	}
	cards := make([]*layoutCard, 0, len(existing)+len(operations))
	for _, current := range existing {
		cards = append(cards, &layoutCard{
			placementID: current.ID,
			widgetID:    current.WidgetID,
			placed: placed{
				position: fmt.Sprintf("card %s", current.ID),
				input:    &gen.PlacementInput{ID: current.ID.String(), WidgetID: current.WidgetID.String(), X: int(current.X), Y: int(current.Y), W: int(current.W), H: int(current.H)},
			},
			changed: false,
		})
	}
	var removed []uuid.UUID

	// A widget is read once however many operations name it: for its chart
	// type, which sets a card's minimum size, and to refuse a widget of
	// another project. The read holds it in share mode until commit.
	chartTypes := map[uuid.UUID]string{}
	chartTypeFor := func(position string, widgetID uuid.UUID) (string, error) {
		if chartType, ok := chartTypes[widgetID]; ok {
			return chartType, nil
		}
		widget, err := queries.GetWidgetForPlacement(ctx, repo.GetWidgetForPlacementParams{ProjectID: *authCtx.ProjectID, ID: widgetID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", oops.E(oops.CodeBadRequest, nil, "%s: widget %s is not in this project", position, widgetID)
			}
			return "", fmt.Errorf("load widget for card: %w", err)
		}
		chartTypes[widgetID] = chartTypeOf(widget.Visualization)
		return chartTypes[widgetID], nil
	}
	findCard := func(position, rawID string) (int, error) {
		placementID, err := uuid.Parse(rawID)
		if err != nil {
			return 0, oops.E(oops.CodeBadRequest, err, "%s: invalid placement id", position)
		}
		index := slices.IndexFunc(cards, func(c *layoutCard) bool { return c.placementID == placementID })
		if index < 0 {
			return 0, oops.E(oops.CodeNotFound, nil, "%s: card %s is not on this dashboard", position, placementID)
		}
		return index, nil
	}

	for i, operation := range operations {
		position := fmt.Sprintf("operations[%d]", i)
		switch operation.Kind {
		case LayoutAddCard:
			widgetID, err := uuid.Parse(operation.WidgetID)
			if err != nil {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, err, "%s: invalid widget id", position)
			}
			chartType, err := chartTypeFor(position, widgetID)
			if err != nil {
				return repo.Dashboard{}, err
			}
			_, opening := cardSizes(chartType)
			var bottom int
			for _, c := range cards {
				bottom = max(bottom, c.placed.input.Y+c.placed.input.H)
			}
			input := &gen.PlacementInput{ID: "", WidgetID: widgetID.String(), X: conv.PtrValOr(operation.X, 0), Y: conv.PtrValOr(operation.Y, bottom), W: conv.PtrValOr(operation.W, opening.w), H: conv.PtrValOr(operation.H, opening.h)}
			if reason := checkPlacement(position, input, chartType); reason != "" {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s", reason)
			}
			cards = append(cards, &layoutCard{placementID: uuid.Nil, widgetID: widgetID, placed: placed{position: position, input: input}, changed: true})
		case LayoutMoveCard:
			index, err := findCard(position, operation.PlacementID)
			if err != nil {
				return repo.Dashboard{}, err
			}
			if operation.X == nil || operation.Y == nil || operation.W == nil || operation.H == nil {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s: moving a card needs x, y, w and h", position)
			}
			card := cards[index]
			chartType, err := chartTypeFor(position, card.widgetID)
			if err != nil {
				return repo.Dashboard{}, err
			}
			input := &gen.PlacementInput{ID: card.placed.input.ID, WidgetID: card.widgetID.String(), X: *operation.X, Y: *operation.Y, W: *operation.W, H: *operation.H}
			if reason := checkPlacement(position, input, chartType); reason != "" {
				return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s", reason)
			}
			card.placed, card.changed = placed{position: position, input: input}, true
		case LayoutRemoveCard:
			index, err := findCard(position, operation.PlacementID)
			if err != nil {
				return repo.Dashboard{}, err
			}
			removed = append(removed, cards[index].placementID)
			cards = slices.Delete(cards, index, index+1)
		default:
			return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s: unknown operation %q", position, operation.Kind)
		}
	}

	if len(cards) > maxCards {
		return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "a dashboard holds at most %d cards", maxCards)
	}
	// The untouched cards go first, so an overlap is reported against the
	// operation that would land on one.
	laid := make([]placed, 0, len(cards))
	for _, c := range cards {
		if !c.changed {
			laid = append(laid, c.placed)
		}
	}
	for _, c := range cards {
		if c.changed {
			laid = append(laid, c.placed)
		}
	}
	if reason := checkOverlaps(laid); reason != "" {
		return repo.Dashboard{}, oops.E(oops.CodeBadRequest, nil, "%s", reason)
	}

	for _, placementID := range removed {
		if _, err := queries.DeletePlacement(ctx, repo.DeletePlacementParams{ProjectID: *authCtx.ProjectID, DashboardID: dashboard.ID, ID: placementID}); err != nil {
			return repo.Dashboard{}, fmt.Errorf("remove dashboard card: %w", err)
		}
	}
	for _, c := range cards {
		if !c.changed {
			continue
		}
		in := c.placed.input
		if c.placementID == uuid.Nil {
			if _, err := queries.InsertPlacement(ctx, repo.InsertPlacementParams{
				ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, DashboardID: dashboard.ID, WidgetID: c.widgetID,
				X: int32(in.X), Y: int32(in.Y), W: int32(in.W), H: int32(in.H), //nolint:gosec // bounded by checkPlacement
			}); err != nil {
				return repo.Dashboard{}, fmt.Errorf("add dashboard card: %w", err)
			}
			continue
		}
		if _, err := queries.MovePlacement(ctx, repo.MovePlacementParams{
			X: int32(in.X), Y: int32(in.Y), W: int32(in.W), H: int32(in.H), //nolint:gosec // bounded by checkPlacement
			ProjectID: *authCtx.ProjectID, DashboardID: dashboard.ID, ID: c.placementID,
		}); err != nil {
			return repo.Dashboard{}, fmt.Errorf("move dashboard card: %w", err)
		}
	}
	return s.touch(ctx, authCtx, queries, dashboard.ID)
}
