package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

type UpdateClickHouseToolSource struct {
	logger           *slog.Logger
	telemetryService *telemetry.Service
}

func NewUpdateClickHouseToolSource(logger *slog.Logger, telemetryService *telemetry.Service) *UpdateClickHouseToolSource {
	return &UpdateClickHouseToolSource{
		logger:           logger,
		telemetryService: telemetryService,
	}
}

type UpdateClickHouseToolSourceArgs struct {
	ProjectID string
	OldSource string
	NewSource string
}

func (u *UpdateClickHouseToolSource) Do(ctx context.Context, args UpdateClickHouseToolSourceArgs) error {
	if args.ProjectID == "" {
		return fmt.Errorf("project ID cannot be empty")
	}
	if args.OldSource == "" {
		return fmt.Errorf("old source cannot be empty")
	}
	if args.NewSource == "" {
		return fmt.Errorf("new source cannot be empty")
	}

	err := u.telemetryService.UpdateToolSourceBulk(ctx, args.ProjectID, args.OldSource, args.NewSource)
	if err != nil {
		return fmt.Errorf("update tool source in ClickHouse: %w", err)
	}

	u.logger.InfoContext(ctx, fmt.Sprintf("updated tool source in ClickHouse: project_id=%s old_source=%s new_source=%s", args.ProjectID, args.OldSource, args.NewSource))

	return nil
}
