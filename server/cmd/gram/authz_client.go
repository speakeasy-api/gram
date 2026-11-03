package gram

import (
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// newAuthzClient builds the typed authorization client over engine the way
// the main server does: the default registry, each migration unit routed by
// its feature flag, shadow comparisons recorded as logs and metrics. A
// registry that does not compile stops the binary at startup.
func newAuthzClient(logger *slog.Logger, meterProvider metric.MeterProvider, engine *authz.Engine, featureFlags feature.Provider) (*authz.Client, error) {
	registry, err := authz.DefaultRegistry()
	if err != nil {
		return nil, fmt.Errorf("compile authorization registry: %w", err)
	}
	router, err := authz.NewMigrationRouter(registry, authz.FeatureFlagModes(featureFlags), authz.DefaultMigrationUnits()...)
	if err != nil {
		return nil, fmt.Errorf("create authorization migration router: %w", err)
	}
	client, err := authz.NewClient(engine, registry,
		authz.WithMigrationRouter(router),
		authz.WithShadowRecorder(authz.NewShadowRecorder(logger, meterProvider)),
		authz.WithBuildVersion(shortGitSHA()),
	)
	if err != nil {
		return nil, fmt.Errorf("create authorization client: %w", err)
	}
	return client, nil
}
