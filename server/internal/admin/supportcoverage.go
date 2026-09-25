package admin

import (
	"context"
	"errors"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// SupportCoverageReader assembles one organization's observed coverage matrix.
// Implemented by the telemetry service, which owns the ClickHouse reads.
type SupportCoverageReader interface {
	SupportCoverageForOrganization(ctx context.Context, organizationID string, windowDays int) (*telemetry.SupportCoverageResult, error)
}

func (s *Service) GetSupportCoverage(ctx context.Context, payload *gen.GetSupportCoveragePayload) (*gen.SupportCoverageResult, error) {
	if s.supportCoverage == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "support coverage is unavailable")
	}

	result, err := s.supportCoverage.SupportCoverageForOrganization(ctx, payload.OrganizationID, payload.WindowDays)
	if err != nil {
		// The reader classifies its own failures — a blank organization id is
		// invalid input, not a server fault — so a shareable error passes
		// through with its code rather than being flattened to a 500.
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
			return nil, shareable
		}
		return nil, oops.E(oops.CodeUnexpected, err, "read support coverage").LogError(ctx, s.logger)
	}

	cells := make([]*gen.SupportCoverageCell, 0, len(result.Cells))
	for _, cell := range result.Cells {
		cells = append(cells, &gen.SupportCoverageCell{
			Capability: cell.Capability,
			Surface:    cell.Surface,
			Status:     cell.Status,
			Value:      cell.Value,
			Detail:     cell.Detail,
			LastSeen:   stampSupportCoverage(cell.LastSeen),
		})
	}

	unmapped := make([]*gen.SupportCoverageUnmapped, 0, len(result.Unmapped))
	for _, item := range result.Unmapped {
		unmapped = append(unmapped, &gen.SupportCoverageUnmapped{HookSource: item.HookSource, Sessions: item.Sessions})
	}

	return &gen.SupportCoverageResult{
		Cells:      cells,
		Unmapped:   unmapped,
		WindowDays: result.WindowDays,
		From:       result.From.UTC().Format(time.RFC3339),
		To:         result.To.UTC().Format(time.RFC3339),
	}, nil
}

// Returns nil rather than an empty string: last_seen declares a date-time
// format, so a sentinel would fail validation for the whole response.
func stampSupportCoverage(at time.Time) *string {
	if at.IsZero() {
		return nil
	}
	stamped := at.UTC().Format(time.RFC3339)
	return &stamped
}
