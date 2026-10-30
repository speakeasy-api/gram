package deviceintegrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/device_integrations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// CoverageBuckets and PolicyStates are the filter values ManagedDevices
// accepts, in the order the management API declares them.
var (
	CoverageBuckets = []string{"agent_active", "agent_stale", "agent_other_device", "no_agent", "no_email", "unresolved_email", "missing"}
	PolicyStates    = []string{"attested", "outdated", "unreported", "halted"}
)

// Reader answers the coverage reads for a named organization. It does no
// authorization: the management API authorizes before calling it, and the
// Platform MCP reads through it after its own live check.
type Reader struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	features feature.Provider
}

func NewReader(logger *slog.Logger, db *pgxpool.Pool, features feature.Provider) *Reader {
	return &Reader{
		logger:   logger.With(attr.SlogComponent("deviceintegrations.reader")),
		db:       db,
		repo:     repo.New(db),
		features: features,
	}
}

// ManagedDevicesQuery selects one page of an organization's inventory. Empty
// strings and empty lists mean no filter.
type ManagedDevicesQuery struct {
	OrganizationID string
	Provider       string
	CoverageBucket string
	PolicyState    string
	UserIDs        []string
	UserEmails     []string
	Cursor         string
	Limit          int
}

// ErrInvalidCursor marks a cursor that is not one this read issued.
var ErrInvalidCursor = errors.New("invalid managed device cursor")

func (r *Reader) ManagedDevices(ctx context.Context, query ManagedDevicesQuery) (*gen.ListManagedDevicesResult, error) {
	cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if query.Cursor != "" {
		parsed, err := uuid.Parse(query.Cursor)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
		}
		cursor = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	rows, err := r.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		ActiveCutoff:   conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow)),
		DeviceLevel:    deviceLevelCoverage(ctx, r.logger, r.db, r.features, query.OrganizationID),
		OrganizationID: query.OrganizationID,
		Provider:       conv.ToPGTextEmpty(query.Provider),
		CursorID:       cursor,
		Bucket:         conv.ToPGTextEmpty(query.CoverageBucket),
		PolicyState:    conv.ToPGTextEmpty(query.PolicyState),
		// Blank entries would leave a non-empty array that matches no device,
		// so a filter of only empty values has to read as no filter at all.
		UserIds:    nonBlank(query.UserIDs),
		UserEmails: nonBlank(query.UserEmails),
		PageLimit:  int32(query.Limit), //nolint:gosec // callers bound limit to [1,200]
	})
	if err != nil {
		return nil, fmt.Errorf("list managed devices: %w", err)
	}

	devices := make([]*gen.ManagedDevice, 0, len(rows))
	for _, row := range rows {
		devices = append(devices, deviceView(ctx, r.logger, row))
	}
	var nextCursor *string
	if len(rows) == query.Limit {
		last := rows[len(rows)-1].ID.String()
		nextCursor = &last
	}
	return &gen.ListManagedDevicesResult{Devices: devices, NextCursor: nextCursor}, nil
}

// Coverage summarizes an organization's coverage, optionally for one
// provider (empty for all).
func (r *Reader) Coverage(ctx context.Context, organizationID string, provider string) (*gen.DeviceIntegrationCoverage, error) {
	// Resolved once and shared: the bucket counts and the unmanaged-users tile
	// sit side by side on the same page, so evaluating the flag twice could
	// render a device as covered next to a tile calling its user unmanaged.
	deviceLevel := deviceLevelCoverage(ctx, r.logger, r.db, r.features, organizationID)

	counts, err := r.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		ActiveCutoff:   conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow)),
		DeviceLevel:    deviceLevel,
		OrganizationID: organizationID,
		Provider:       conv.ToPGTextEmpty(provider),
	})
	if err != nil {
		return nil, fmt.Errorf("compute device integration coverage: %w", err)
	}
	unmanaged, err := r.repo.CountUnmanagedAgentUsers(ctx, repo.CountUnmanagedAgentUsersParams{
		DeviceLevel:    deviceLevel,
		OrganizationID: organizationID,
		Provider:       conv.ToPGTextEmpty(provider),
	})
	if err != nil {
		return nil, fmt.Errorf("count unmanaged agent users: %w", err)
	}

	return &gen.DeviceIntegrationCoverage{
		OrganizationID:            organizationID,
		ActiveWindowMinutes:       int(activeWindow / time.Minute),
		Attestation:               coverageAttestation(deviceLevel, counts.AgentActive, counts.AgentActiveDeviceAttested),
		AgentActive:               counts.AgentActive,
		AgentActiveDeviceAttested: counts.AgentActiveDeviceAttested,
		AgentStale:                counts.AgentStale,
		AgentOtherDevice:          counts.AgentOtherDevice,
		NoAgent:                   counts.NoAgent,
		NoEmail:                   counts.NoEmail,
		UnresolvedEmail:           counts.UnresolvedEmail,
		Missing:                   counts.Missing,
		TotalDevices:              counts.Total,
		UnmanagedAgentUsers:       unmanaged,
		PolicyAttested:            counts.PolicyAttested,
		PolicyOutdated:            counts.PolicyOutdated,
		PolicyUnreported:          counts.PolicyUnreported,
		EnforcementHalted:         counts.EnforcementHalted,
	}, nil
}
