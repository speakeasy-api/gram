package gram

import (
	"context"
	"fmt"
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

func newPICascade(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, client openrouter.CompletionClient, policy *guardian.Policy, provisioner openrouter.Provisioner, db repo.DBTX) *piopenrouter.Cascade {
	jev := typesafe.New(policy.PooledClient(), func(ctx context.Context, orgID string) (string, error) {
		key, err := provisioner.ProvisionAPIKey(ctx, orgID, openrouter.KeyTypeInternal)
		if err != nil {
			return "", fmt.Errorf("provision Jev internal key: %w", err)
		}
		return key, nil
	})
	return piopenrouter.NewCascade(logger, tracerProvider, meterProvider, client, jev, judgemessage.NewWindowLoader(db).Load)
}
