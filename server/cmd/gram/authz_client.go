package gram

import (
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// newAuthzClient builds the typed authorization client over engine. It
// compiles the default registry and migration units, so a broken declaration
// stops the command at startup instead of failing checks later. Each unit's
// feature flag routes it, and every unit starts with the engine deciding.
func newAuthzClient(logger *slog.Logger, meterProvider metric.MeterProvider, engine *authz.Engine, flags feature.Provider) (*authz.Client, error) {
	registry, err := authz.DefaultRegistry()
	if err != nil {
		return nil, fmt.Errorf("compile authorization registry: %w", err)
	}
	router, err := authz.NewMigrationRouter(registry, authz.FeatureFlagModes(flags), authz.DefaultMigrationUnits()...)
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
