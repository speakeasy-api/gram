package dataexports

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	// ErrRouteNotFound means no live route with that id exists in the named
	// organization and project.
	ErrRouteNotFound = errors.New("data export route not found")

	// ErrRouteDestinationRequired means a route would be enabled while it has no
	// destination, so there would be nowhere to send its data.
	ErrRouteDestinationRequired = errors.New("a data export route needs a destination before it can be enabled")

	// ErrRouteDestinationInactive means the route names a destination that is
	// deleted or belongs to another project.
	ErrRouteDestinationInactive = errors.New("the data export route's destination is not an active destination in this project")
)

// RouteEnabledCore pauses and resumes one route. It changes the route's
// enabled flag and nothing else: the data source and the destination are not
// part of its input and the statement it runs cannot write them.
//
// Resuming enforces the same invariant the dashboard's route update does — an
// enabled route must name an active, readable destination in its project — so
// neither surface can enable a route with nowhere to send its data.
type RouteEnabledCore struct {
	audit      *audit.Logger
	encryption *encryption.Client
}

// NewRouteEnabledCore returns nil when a dependency is missing, so a caller
// composing it can register an unavailable tool instead of a broken one.
func NewRouteEnabledCore(auditLogger *audit.Logger, encryptionClient *encryption.Client) *RouteEnabledCore {
	if auditLogger == nil || encryptionClient == nil {
		return nil
	}
	return &RouteEnabledCore{audit: auditLogger, encryption: encryptionClient}
}

// SetRouteEnabledParams names one route exactly and the state it should end
// in. The actor is recorded on the audit entry.
type SetRouteEnabledParams struct {
	OrganizationID string
	ProjectID      uuid.UUID
	RouteID        uuid.UUID
	Enabled        bool

	Actor            urn.Principal
	ActorDisplayName *string
}

// SetRouteEnabledResult carries the committed route row. Changed is false when
// the route was already in the requested state, in which case nothing was
// written and no audit entry was recorded.
type SetRouteEnabledResult struct {
	Before  repo.DataExportRoute
	After   repo.DataExportRoute
	Changed bool
}

// SetEnabled locks the route row and flips its enabled flag inside tx. The
// caller owns the transaction, so the change, its audit entry, and anything
// else the caller records (an idempotency receipt) commit together.
func (c *RouteEnabledCore) SetEnabled(ctx context.Context, tx pgx.Tx, params SetRouteEnabledParams) (SetRouteEnabledResult, error) {
	if c == nil || c.audit == nil || c.encryption == nil {
		return SetRouteEnabledResult{}, errors.New("data export route enabled core is not composed")
	}
	queries := repo.New(tx)
	before, err := queries.GetDataExportRouteForUpdate(ctx, repo.GetDataExportRouteForUpdateParams{
		OrganizationID: params.OrganizationID,
		ProjectID:      params.ProjectID,
		ID:             params.RouteID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SetRouteEnabledResult{}, ErrRouteNotFound
	}
	if err != nil {
		return SetRouteEnabledResult{}, fmt.Errorf("lock data export route: %w", err)
	}
	if before.Enabled == params.Enabled {
		return SetRouteEnabledResult{Before: before, After: before, Changed: false}, nil
	}
	if params.Enabled {
		if err := checkRouteDestination(ctx, queries, c.encryption, params.OrganizationID, params.ProjectID, before.OtelDestinationID, true); err != nil {
			return SetRouteEnabledResult{}, err
		}
	}
	after, err := queries.SetDataExportRouteEnabled(ctx, repo.SetDataExportRouteEnabledParams{
		Enabled:        params.Enabled,
		OrganizationID: params.OrganizationID,
		ProjectID:      params.ProjectID,
		ID:             params.RouteID,
	})
	if err != nil {
		return SetRouteEnabledResult{}, fmt.Errorf("set data export route enabled: %w", err)
	}

	if params.Enabled {
		err = c.audit.LogDataExportRouteResume(ctx, tx, audit.LogDataExportRouteResumeEvent{
			OrganizationID:      params.OrganizationID,
			ProjectID:           params.ProjectID,
			Actor:               params.Actor,
			ActorDisplayName:    params.ActorDisplayName,
			ActorSlug:           nil,
			RouteURN:            urn.NewDataExportRoute(after.ID),
			DataSource:          after.DataSource,
			RouteSnapshotBefore: routeSnapshot(before),
			RouteSnapshotAfter:  routeSnapshot(after),
		})
	} else {
		err = c.audit.LogDataExportRoutePause(ctx, tx, audit.LogDataExportRoutePauseEvent{
			OrganizationID:      params.OrganizationID,
			ProjectID:           params.ProjectID,
			Actor:               params.Actor,
			ActorDisplayName:    params.ActorDisplayName,
			ActorSlug:           nil,
			RouteURN:            urn.NewDataExportRoute(after.ID),
			DataSource:          after.DataSource,
			RouteSnapshotBefore: routeSnapshot(before),
			RouteSnapshotAfter:  routeSnapshot(after),
		})
	}
	if err != nil {
		return SetRouteEnabledResult{}, fmt.Errorf("audit data export route enabled change: %w", err)
	}
	return SetRouteEnabledResult{Before: before, After: after, Changed: true}, nil
}

// checkRouteDestination is the one place that decides whether a route may
// point at a destination, shared by the dashboard's route create and update
// and by RouteEnabledCore. An enabled route needs a destination; any named
// destination must be live in the same project and hold a usable stored
// configuration. The destination row is held FOR SHARE until tx ends, so a
// concurrent destination delete cannot land between this check and the write.
func checkRouteDestination(
	ctx context.Context,
	queries *repo.Queries,
	encryptionClient *encryption.Client,
	organizationID string,
	projectID uuid.UUID,
	destinationID uuid.NullUUID,
	enabled bool,
) error {
	if !destinationID.Valid {
		if enabled {
			return ErrRouteDestinationRequired
		}
		return nil
	}
	destination, err := queries.GetOtelDestinationForRoute(ctx, repo.GetOtelDestinationForRouteParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ID:             destinationID.UUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRouteDestinationInactive
	}
	if err != nil {
		return fmt.Errorf("load route destination: %w", err)
	}
	if _, err := validateDestinationURL(destination.EndpointUrl); err != nil {
		return fmt.Errorf("stored OTEL destination has invalid endpoint URL: %w", err)
	}
	if _, err := sensitiveDataFromRow(destination.SensitiveData); err != nil {
		return fmt.Errorf("stored OTEL destination has invalid sensitive-data policy: %w", err)
	}
	if _, err := decryptHeaders(encryptionClient, destination.HeadersEncrypted); err != nil {
		return fmt.Errorf("decode stored OTEL destination headers: %w", err)
	}
	return nil
}
