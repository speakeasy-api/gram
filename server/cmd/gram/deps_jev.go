package gram

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/pubsub/v2"
	"go.opentelemetry.io/otel/metric"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/scanners/jev"
	"github.com/speakeasy-api/gram/server/internal/scanners/judgeshadow"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

func newJudgeShadowTopic(ctx context.Context, broker pubSubBroker) (gcp.Publisher[*riskv1.JudgeShadowAnalysis], error) {
	settings := pubsub.DefaultPublishSettings
	settings.FlowControlSettings.MaxOutstandingMessages = 256
	settings.FlowControlSettings.MaxOutstandingBytes = 8 << 20
	settings.FlowControlSettings.LimitExceededBehavior = pubsub.FlowControlSignalError
	pub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.JudgeShadowAnalysis{}, gcp.WithPubSubPublishSettings(&settings))
	if err != nil {
		return nil, fmt.Errorf("create Jev shadow publisher: %w", err)
	}
	return pub, nil
}

func newJudgeShadowHandler(logger *slog.Logger, flags feature.Provider, policy *guardian.Policy, meter metric.MeterProvider, provisioner openrouter.Provisioner) (*judgeshadow.Handler, error) {
	evaluator := typesafe.New(policy.PooledClient(), func(ctx context.Context, orgID string) (string, error) {
		key, err := provisioner.ProvisionAPIKey(ctx, orgID, openrouter.KeyTypeInternal)
		if err != nil {
			return "", fmt.Errorf("provision Jev internal key: %w", err)
		}
		return key, nil
	})
	handler, err := judgeshadow.NewHandler(logger, flags, jev.New(evaluator), meter)
	if err != nil {
		return nil, fmt.Errorf("create Jev shadow handler: %w", err)
	}
	return handler, nil
}
