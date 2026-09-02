package gram

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
)

func newHostedInferenceCheckpoint(db *pgxpool.Pool, meterProvider metric.MeterProvider, logger *slog.Logger) (*hostedinference.Checkpoint, error) {
	registry, err := mcptoolexecution.NewRegistry(db)
	if err != nil {
		return nil, fmt.Errorf("create hosted-inference registry: %w", err)
	}
	checkpoint, err := hostedinference.NewProductionCheckpoint(db, registry, hostedinference.DefaultEvaluationTimeout, meterProvider, logger)
	if err != nil {
		return nil, fmt.Errorf("create hosted-inference checkpoint: %w", err)
	}
	return checkpoint, nil
}
