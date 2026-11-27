package gram

import (
	"context"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// newPICascade builds the prompt injection cascade. When OpenRouter serves no
// Decisions models on the partition's inference host, the Jev prefilter is
// unavailable and every message is labeled unavailable rather than sending
// prompts outside the partition.
func newPICascade(ctx context.Context, logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, client openrouter.CompletionClient, policy *guardian.Policy, provisioner openrouter.Provisioner, endpoints openrouter.Endpoints, db repo.DBTX, transcripts *transcriptstore.Transcripts) *piopenrouter.Cascade {
	var jev typesafe.Evaluator = typesafe.Unavailable{}
	if endpoints.ServesDecisionModels {
		jev = typesafe.New(policy.PooledClient(), func(ctx context.Context, orgID string) (string, error) {
			key, err := provisioner.ProvisionAPIKey(ctx, orgID, openrouter.KeyTypeInternal)
			if err != nil {
				return "", fmt.Errorf("provision Jev internal key: %w", err)
			}
			return key, nil
		}, endpoints.Inference)
	} else {
		logger.WarnContext(ctx, "prompt injection detection is unavailable: OpenRouter serves no Decisions models in this partition")
	}

	return piopenrouter.NewCascade(logger, tracerProvider, meterProvider, client, jev, judgemessage.NewWindowLoader(db, transcripts).Load)
}
