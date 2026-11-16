package dataexports

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrRouteMinSeverityInvalid means the level is not one a route accepts, or the
// route's data source has no level.
var ErrRouteMinSeverityInvalid = errors.New("invalid data export route minimum level")

// RouteLevelCore changes the minimum level of one MCP connection logs route and
// nothing else.
type RouteLevelCore struct {
	audit *audit.Logger
}

// NewRouteLevelCore returns nil without an audit logger, so a caller can
// register an unavailable tool instead of a broken one.
func NewRouteLevelCore(auditLogger *audit.Logger) *RouteLevelCore {
	if auditLogger == nil {
		return nil
	}
	return &RouteLevelCore{audit: auditLogger}
}

type SetRouteMinSeverityParams struct {
	OrganizationID string
	ProjectID      uuid.UUID
	RouteID        uuid.UUID
	MinSeverity    string

	Actor            urn.Principal
	ActorDisplayName *string
}

// SetRouteMinSeverityResult is unchanged, with no write or audit entry, when
// the route already had the requested level.
type SetRouteMinSeverityResult struct {
	Route   repo.DataExportRoute
	Before  string
	After   string
	Changed bool
}

// SetMinSeverity locks the route inside tx, stores its level, and records the
// change. The caller owns the transaction.
func (c *RouteLevelCore) SetMinSeverity(ctx context.Context, tx pgx.Tx, params SetRouteMinSeverityParams) (SetRouteMinSeverityResult, error) {
	if c == nil || c.audit == nil {
		return SetRouteMinSeverityResult{}, errors.New("data export route level core is not composed")
	}
	queries := repo.New(tx)
	route, err := queries.GetDataExportRouteForUpdate(ctx, repo.GetDataExportRouteForUpdateParams{
		OrganizationID: params.OrganizationID,
		ProjectID:      params.ProjectID,
		ID:             params.RouteID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SetRouteMinSeverityResult{}, ErrRouteNotFound
	}
	if err != nil {
		return SetRouteMinSeverityResult{}, fmt.Errorf("lock data export route: %w", err)
	}
	level, err := NormalizeMinSeverity(route.DataSource, params.MinSeverity)
	if err != nil {
		return SetRouteMinSeverityResult{}, fmt.Errorf("%w: %w", ErrRouteMinSeverityInvalid, err)
	}
	before, err := routeMinSeverity(ctx, queries, params.OrganizationID, params.ProjectID, route)
	if err != nil {
		return SetRouteMinSeverityResult{}, err
	}
	if before == nil {
		return SetRouteMinSeverityResult{}, fmt.Errorf("data export route %s has no minimum level", route.ID)
	}
	if *before == level {
		return SetRouteMinSeverityResult{Route: route, Before: level, After: level, Changed: false}, nil
	}
	after, err := queries.UpsertMCPConnectionLogRouteSettings(ctx, repo.UpsertMCPConnectionLogRouteSettingsParams{
		OrganizationID:    params.OrganizationID,
		ProjectID:         params.ProjectID,
		MinSeverity:       level,
		DataExportRouteID: route.ID,
	})
	if err != nil {
		return SetRouteMinSeverityResult{}, fmt.Errorf("set MCP connection log level: %w", err)
	}
	if err := c.audit.LogDataExportRouteUpdate(ctx, tx, audit.LogDataExportRouteUpdateEvent{
		OrganizationID:      params.OrganizationID,
		ProjectID:           params.ProjectID,
		Actor:               params.Actor,
		ActorDisplayName:    params.ActorDisplayName,
		ActorSlug:           nil,
		RouteURN:            urn.NewDataExportRoute(route.ID),
		DataSource:          route.DataSource,
		RouteSnapshotBefore: routeSnapshot(route, before),
		RouteSnapshotAfter:  routeSnapshot(route, &after),
	}); err != nil {
		return SetRouteMinSeverityResult{}, fmt.Errorf("audit data export route level change: %w", err)
	}
	return SetRouteMinSeverityResult{Route: route, Before: *before, After: after, Changed: true}, nil
}
