package gram

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/pubsub/v2"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/metric"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/scanners/jev"
	"github.com/speakeasy-api/gram/server/internal/scanners/judgeshadow"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

func jevFlags() []cli.Flag {
	return []cli.Flag{&cli.StringFlag{Name: "typesafe-api-key", EnvVars: []string{"TYPESAFE_API_KEY"}, Usage: "TypeSafe API key for Jev shadow evaluation"}}
}

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

func newJudgeShadowHandler(c *cli.Context, logger *slog.Logger, flags feature.Provider, policy *guardian.Policy, meter metric.MeterProvider) (*judgeshadow.Handler, error) {
	var evaluator typesafe.Evaluator = typesafe.Unavailable{}
	if key := c.String("typesafe-api-key"); key != "" {
		evaluator = typesafe.New(policy.PooledClient(), key)
	}
	return judgeshadow.NewHandler(logger, flags, jev.New(evaluator), meter)
}
