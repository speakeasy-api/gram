package assistants

import (
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	// RuntimeProviderGKE launches new runtimes on the GKE Agent Sandbox cluster.
	RuntimeProviderGKE = runtimeBackendGKE

	// RuntimeProviderLocal launches new runtimes as Docker containers on the
	// developer's machine.
	RuntimeProviderLocal = runtimeBackendLocal

	// RuntimeProviderDisabled turns assistants off for the deployment: no new
	// runtime is ever admitted, and assistant APIs that need a runtime return
	// ErrRuntimeDisabled. Backends that are still configured stay reachable so
	// rows they own can drain.
	RuntimeProviderDisabled = "disabled"
)

// ErrRuntimeDisabled reports that the deployment runs with the disabled
// assistant runtime provider, so the requested work cannot run.
var ErrRuntimeDisabled = errors.New("assistants are not available on this deployment")

type RuntimeBackendConfig struct {
	Provider string
	GKE      GKERuntimeConfig
	Local    LocalRuntimeConfig
}

// NewRuntimeBackend assembles a runtimeRouter over configured GKE and local
// backends and targets config.Provider for new admissions. The target backend
// must be among those constructed, unless the target is
// RuntimeProviderDisabled, which needs no backend at all.
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

	if config.Provider == RuntimeProviderDisabled {
		return newDisabledRuntimeBackend(backends)
	}
	router, err := newRuntimeRouter(config.Provider, backends)
	if err != nil {
		return nil, fmt.Errorf("assemble assistant runtime backends: %w", err)
	}
	return router, nil
}

// newDisabledRuntimeBackend routes rows to the backends that are still
// configured so they can drain, without the Kit runtime. Kit is never
// installed with the disabled provider, and Kit rows carry the same backend
// name as legacy GKE rows, so a plain router would hand them to the legacy
// GKE backend, which cannot tear down Kit resources. Routing by runtime
// version instead leaves Kit rows refused and unreaped.
func newDisabledRuntimeBackend(backends map[string]RuntimeBackend) (RuntimeBackend, error) {
	router, err := newRuntimeRouter(RuntimeProviderDisabled, backends)
	if err != nil {
		return nil, fmt.Errorf("assemble assistant runtime backends: %w", err)
	}
	return &versionedRuntimeBackend{legacy: router, kit: nil}, nil
}
