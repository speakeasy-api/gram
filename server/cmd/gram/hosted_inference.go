package gram

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
)

type aiAccessEnforcement struct {
	registry        *killswitches.Registry
	evaluator       *killswitches.Evaluator
	hostedInference *hostedinference.Checkpoint
}

// newAIAccessEnforcement constructs one code-owned registry and evaluator per
// production process. Hooks, LiteLLM, and hosted inference share these exact
// instances; checkpoints add only surface-specific adapters and timeouts.
func newAIAccessEnforcement(db *pgxpool.Pool, meterProvider metric.MeterProvider, logger *slog.Logger) (*aiAccessEnforcement, error) {
	registry, err := mcptoolexecution.NewRegistry(db)
	if err != nil {
		return nil, fmt.Errorf("create ai_access registry: %w", err)
	}
	evaluator, err := killswitches.NewEvaluator(db, registry, hooks.AIAccessEvaluationTimeout, meterProvider, logger)
	if err != nil {
		return nil, fmt.Errorf("create ai_access evaluator: %w", err)
	}
	checkpoint, err := hostedinference.NewCheckpoint(registry, evaluator, hostedinference.DefaultEvaluationTimeout)
	if err != nil {
		return nil, fmt.Errorf("create hosted-inference checkpoint: %w", err)
	}
	return &aiAccessEnforcement{registry: registry, evaluator: evaluator, hostedInference: checkpoint}, nil
}
