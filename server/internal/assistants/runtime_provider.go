package assistants

import (
	"fmt"
	"log/slog"
	"net/url"

	"github.com/superfly/fly-go/tokens"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	RuntimeProviderFlyIO = runtimeBackendFlyIO
	RuntimeProviderGKE   = runtimeBackendGKE

	defaultFlyRuntimeRegion = "us"
	defaultFlyRuntimePrefix = "gram-asst"
)

type RuntimeBackendConfig struct {
	Provider string
	Fly      FlyRuntimeConfig
	GKE      GKERuntimeConfig
}

type FlyRuntimeConfig struct {
	ServiceName        string
	ServiceVersion     string
	FlyTokens          *tokens.Tokens
	FlyAPIURL          string
	FlyMachinesBaseURL string
	DefaultFlyOrg      string
	DefaultFlyRegion   string
	OCIImage           string
	ImageTag           string
	AppNamePrefix      string
	ServerURL          *url.URL

	// OTLP exporter settings stamped into runtime machine env as standard
	// OTEL_EXPORTER_OTLP_* variables so the runner exports agentkit spans.
	// Trace export is disabled when OTLPEndpoint is empty.
	OTLPEndpoint string
	OTLPProtocol string
	OTLPHeaders  string
	// Environment is stamped into OTEL_RESOURCE_ATTRIBUTES as
	// deployment.environment.name, matching the server's own resource tags.
	Environment string
}

func (c FlyRuntimeConfig) Validate() error {
	if c.FlyTokens == nil {
		return fmt.Errorf("--assistant-runtime-flyio-api-token is required")
	}
	if c.DefaultFlyOrg == "" {
		return fmt.Errorf("--assistant-runtime-flyio-org is required")
	}
	if c.OCIImage == "" {
		return fmt.Errorf("--assistant-runtime-oci-image is required")
	}
	if c.ServerURL == nil {
		return fmt.Errorf("assistant fly runtime server URL is not configured")
	}
	if c.ServerURL.Hostname() == "" {
		return fmt.Errorf("assistant fly runtime requires a public --assistant-runtime-server-url or --server-url; got %q", c.ServerURL.String())
	}
	switch c.OTLPProtocol {
	case "", "grpc", "http/protobuf", "http/json":
	default:
		return fmt.Errorf("invalid --assistant-runtime-otlp-protocol: %s (allowed: grpc, http/protobuf, http/json)", c.OTLPProtocol)
	}
	return nil
}

// NewRuntimeBackend assembles a runtimeRouter over every configured backend and
// targets config.Provider for new admissions. A backend is constructed when its
// config is present — Fly when an API token is set, GKE when an in-cluster
// kubernetes client is injected — so the two can run side by side (e.g. target
// GKE while still tearing down Fly-backed rows). The target backend must be
// among those constructed.
func NewRuntimeBackend(logger *slog.Logger, tracerProvider trace.TracerProvider, httpPolicy *guardian.Policy, config RuntimeBackendConfig) (RuntimeBackend, error) {
	backends := map[string]RuntimeBackend{}

	if config.Fly.FlyTokens != nil {
		if err := config.Fly.Validate(); err != nil {
			return nil, fmt.Errorf("invalid fly assistant runtime config: %w", err)
		}
		backends[runtimeBackendFlyIO] = NewFlyRuntimeBackend(logger, tracerProvider, httpPolicy, config.Fly)
	}

	if config.GKE.Dynamic != nil {
		if err := config.GKE.Validate(); err != nil {
			return nil, fmt.Errorf("invalid gke assistant runtime config: %w", err)
		}
		// Reach the in-pod runner through the guardian egress policy, matching
		// the Fly backend. The policy must permit the in-cluster sandbox
		// network for traffic to land.
		backends[runtimeBackendGKE] = NewGKERuntimeBackend(logger, tracerProvider, httpPolicy.PooledClient(guardian.WithDefaultRetryConfig()), config.GKE)
	}

	router, err := newRuntimeRouter(config.Provider, backends)
	if err != nil {
		return nil, fmt.Errorf("assemble assistant runtime backends: %w", err)
	}
	return router, nil
}
