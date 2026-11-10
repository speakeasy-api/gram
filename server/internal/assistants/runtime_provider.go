package assistants

import (
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	RuntimeProviderGKE   = runtimeBackendGKE
	RuntimeProviderLocal = runtimeBackendLocal
)

type RuntimeBackendConfig struct {
	Provider string
	GKE      GKERuntimeConfig
	Local    LocalRuntimeConfig
}

// NewRuntimeBackend assembles a runtimeRouter over configured GKE and local
// backends and targets config.Provider for new admissions. The target backend
// must be among those constructed.
func NewRuntimeBackend(logger *slog.Logger, tracerProvider trace.TracerProvider, httpPolicy *guardian.Policy, config RuntimeBackendConfig) (RuntimeBackend, error) {
	backends := map[string]RuntimeBackend{}

	if config.GKE.Dynamic != nil {
		if err := config.GKE.Validate(); err != nil {
			return nil, fmt.Errorf("invalid gke assistant runtime config: %w", err)
		}
		// Reach the in-pod runner through the guardian egress policy and
		// allowlist the runner pod CIDR: the server dials
		// runners by their RFC1918 pod IP (resolved from the Kubernetes API),
		// which the default policy would otherwise reject as SSRF.
		gkeClient := httpPolicy.PooledClient(
			guardian.WithDefaultRetryConfig(),
			guardian.WithAllowedCIDRBlocks(config.GKE.RunnerCIDRBlocks...),
		)
		backends[runtimeBackendGKE] = NewGKERuntimeBackend(logger, tracerProvider, gkeClient, config.GKE)
	}

	if config.Local.Enabled {
		if err := config.Local.Validate(); err != nil {
			return nil, fmt.Errorf("invalid local assistant runtime config: %w", err)
		}
		// Containers publish the runner guest port on a loopback ephemeral
		// port, which the default egress policy blocks. Allowlist loopback for
		// this one client only — the policy's global enforcement is unchanged.
		localClient := httpPolicy.PooledClient(
			guardian.WithDefaultRetryConfig(),
			guardian.WithAllowedCIDRBlocks(localRuntimeLoopbackCIDRs...),
		)
		backends[runtimeBackendLocal] = NewLocalRuntimeBackend(logger, tracerProvider, localClient, newDockerCLIEngine(config.Local.GuestPort), config.Local)
	}

	router, err := newRuntimeRouter(config.Provider, backends)
	if err != nil {
		return nil, fmt.Errorf("assemble assistant runtime backends: %w", err)
	}
	return router, nil
}
