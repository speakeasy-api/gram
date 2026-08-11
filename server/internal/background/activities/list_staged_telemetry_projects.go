package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// ListStagedTelemetryProjects lists the projects with rows waiting in
// telemetry_logs_staging, for the scheduled sweep to fan out one
// PromoteStagedTelemetry pass per project.
type ListStagedTelemetryProjects struct {
	logger    *slog.Logger
	telemetry *telemetryrepo.Queries
}

func NewListStagedTelemetryProjects(logger *slog.Logger, chConn clickhouse.Conn) *ListStagedTelemetryProjects {
	return &ListStagedTelemetryProjects{
		logger:    logger.With(attr.SlogComponent("list_staged_telemetry_projects")),
		telemetry: telemetryrepo.New(chConn),
	}
}

func (l *ListStagedTelemetryProjects) Do(ctx context.Context) ([]PromoteStagedTelemetryArgs, error) {
	projects, err := l.telemetry.ListStagedTelemetryProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list staged telemetry projects: %w", err)
	}
	out := make([]PromoteStagedTelemetryArgs, 0, len(projects))
	for _, project := range projects {
		projectID, err := uuid.Parse(project)
		if err != nil {
			continue
		}
		out = append(out, PromoteStagedTelemetryArgs{ProjectID: projectID})
	}
	return out, nil
}
