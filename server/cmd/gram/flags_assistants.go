package gram

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/superfly/fly-go/tokens"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/k8s"
)

var assistantRuntimeFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "assistant-runtime-provider",
		Usage:   "Assistant runtime provider. Allowed values: flyio, gke.",
		Value:   assistants.RuntimeProviderFlyIO,
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_PROVIDER"},
		Action: func(_ *cli.Context, val string) error {
			switch val {
			case "", assistants.RuntimeProviderFlyIO, assistants.RuntimeProviderGKE:
				return nil
			default:
				return fmt.Errorf("invalid assistant runtime provider: %s", val)
			}
		},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-gke-namespace",
		Usage:   "Kubernetes namespace for GKE Agent Sandbox assistant runtimes. Defaults to gram-{environment}.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GKE_NAMESPACE"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-gke-sandbox-template",
		Usage:   "SandboxTemplate name that GKE assistant runtime claims reference (a SandboxWarmPool on the same template pre-warms pods).",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GKE_SANDBOX_TEMPLATE"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-gke-cluster-endpoint",
		Usage:   "API endpoint (host or IP) of the assistant runtime cluster. Setting this enables the GKE backend.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GKE_CLUSTER_ENDPOINT"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-gke-cluster-ca",
		Usage:   "Base64-encoded CA certificate of the assistant runtime cluster.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GKE_CLUSTER_CA"},
	},
	&cli.StringSliceFlag{
		Name:    "assistant-runtime-gke-runner-cidr",
		Usage:   "Pod CIDR(s) the assistant runner pods are reachable on. The server dials runners by pod IP, so these are allowlisted past the guardian egress policy (which blocks RFC1918 by default).",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GKE_RUNNER_CIDR"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-gke-workspace-volume-name",
		Usage:   "Generic ephemeral volume name mounted as the GKE assistant workspace.",
		Value:   "workspace",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GKE_WORKSPACE_VOLUME_NAME"},
	},
	&cli.Int64Flag{
		Name:    "assistant-runtime-gke-workspace-growth-increment-gib",
		Usage:   "GiB added to a GKE assistant workspace for each authorized growth request.",
		Value:   10,
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GKE_WORKSPACE_GROWTH_INCREMENT_GIB"},
	},
	&cli.Int64Flag{
		Name:    "assistant-runtime-gke-workspace-max-size-gib",
		Usage:   "Maximum requested size in GiB for one GKE assistant workspace.",
		Value:   60,
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GKE_WORKSPACE_MAX_SIZE_GIB"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-server-url",
		Usage:   "Optional host-reachable server base URL for assistant runtimes. Defaults to --server-url.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_SERVER_URL"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-flyio-api-token",
		Usage:   "An organization-scoped API token to use when provisioning assistant runtimes on fly.io.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_FLYIO_API_TOKEN"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-flyio-org",
		Usage:   "The default fly.io organization to deploy assistant runtimes to.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_FLYIO_ORG"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-flyio-region",
		Usage:   "The default fly.io region to deploy assistant runtimes to.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_FLYIO_REGION"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-flyio-app-name-prefix",
		Usage:   "Prefix for fly.io assistant runtime app names.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_FLYIO_APP_NAME_PREFIX"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-oci-image",
		Usage:   "The OCI image repository for the assistant runtime image. It must not include a tag.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_OCI_IMAGE"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-otlp-endpoint",
		Usage:   "OTLP endpoint assistant runtimes export traces to. Trace export is disabled when unset.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_OTLP_ENDPOINT"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-otlp-protocol",
		Usage:   "OTLP transport for assistant runtime traces. Allowed values: grpc, http/protobuf, http/json.",
		Value:   "grpc",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_OTLP_PROTOCOL"},
		Action: func(_ *cli.Context, val string) error {
			switch val {
			case "", "grpc", "http/protobuf", "http/json":
				return nil
			default:
				return fmt.Errorf("invalid assistant runtime otlp protocol: %s", val)
			}
		},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-otlp-headers",
		Usage:   "Headers for the assistant runtime OTLP exporter as comma-separated key=value pairs.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_OTLP_HEADERS"},
	},
}

func assistantRuntimeConfigFromCLI(c *cli.Context, serverURL *url.URL) (assistants.RuntimeBackendConfig, error) {
	resolvedServerURL := serverURL
	if raw := c.String("assistant-runtime-server-url"); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil {
			return assistants.RuntimeBackendConfig{}, fmt.Errorf("parse --assistant-runtime-server-url: %w", err)
		}
		resolvedServerURL = parsed
	}

	provider := c.String("assistant-runtime-provider")
	switch provider {
	case "", assistants.RuntimeProviderFlyIO:
		provider = assistants.RuntimeProviderFlyIO
	case assistants.RuntimeProviderGKE:
	default:
		return assistants.RuntimeBackendConfig{}, fmt.Errorf("invalid assistant runtime provider: %s", provider)
	}

	gkeNamespace := c.String("assistant-runtime-gke-namespace")
	if gkeNamespace == "" {
		gkeNamespace = "gram-" + c.String("environment")
	}

	// tokens.Parse returns a non-nil value even for an empty string, so guard on
	// the raw flag: a GKE-only deployment leaves the Fly token unset and must not
	// have the Fly backend (and its validation) forced on.
	var flyTokens *tokens.Tokens
	if raw := c.String("assistant-runtime-flyio-api-token"); raw != "" {
		flyTokens = tokens.Parse(raw)
	}

	return assistants.RuntimeBackendConfig{
		Provider: provider,
		Fly: assistants.FlyRuntimeConfig{
			ServiceName:        "gram",
			ServiceVersion:     GitSHA,
			FlyTokens:          flyTokens,
			FlyAPIURL:          "",
			FlyMachinesBaseURL: "",
			DefaultFlyOrg:      c.String("assistant-runtime-flyio-org"),
			DefaultFlyRegion:   c.String("assistant-runtime-flyio-region"),
			OCIImage:           c.String("assistant-runtime-oci-image"),
			ImageTag:           AssistantRuntimeImageHash,
			AppNamePrefix:      c.String("assistant-runtime-flyio-app-name-prefix"),
			ServerURL:          resolvedServerURL,
			OTLPEndpoint:       c.String("assistant-runtime-otlp-endpoint"),
			OTLPProtocol:       c.String("assistant-runtime-otlp-protocol"),
			OTLPHeaders:        c.String("assistant-runtime-otlp-headers"),
			Environment:        c.String("environment"),
		},
		// Dynamic is injected in newAssistantRuntime from a remote client for the
		// assistant cluster (k8s.NewRemoteDynamicClient); the egress-controlled
		// HTTP client is built from the guardian policy in NewRuntimeBackend.
		GKE: assistants.GKERuntimeConfig{
			Dynamic:                     nil,
			Namespace:                   gkeNamespace,
			SandboxTemplate:             c.String("assistant-runtime-gke-sandbox-template"),
			GuestPort:                   0,
			OCIImage:                    c.String("assistant-runtime-oci-image"),
			ImageTag:                    AssistantRuntimeImageHash,
			ServerURL:                   resolvedServerURL,
			RunnerCIDRBlocks:            c.StringSlice("assistant-runtime-gke-runner-cidr"),
			WorkspaceVolumeName:         c.String("assistant-runtime-gke-workspace-volume-name"),
			WorkspaceGrowthIncrementGiB: c.Int64("assistant-runtime-gke-workspace-growth-increment-gib"),
			WorkspaceMaxSizeGiB:         c.Int64("assistant-runtime-gke-workspace-max-size-gib"),
		},
	}, nil
}

// newAssistantRuntime resolves CLI flags into an assistant RuntimeBackend.
func newAssistantRuntime(
	ctx context.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	c *cli.Context,
	guardianPolicy *guardian.Policy,
	_ *pgxpool.Pool,
	serverURL *url.URL,
) (assistants.RuntimeBackend, error) {
	cfg, err := assistantRuntimeConfigFromCLI(c, serverURL)
	if err != nil {
		return nil, err
	}

	// Build the GKE backend whenever an assistant cluster is configured (its
	// endpoint is set), not only when it is the target: a fly-target process must
	// still reach the assistant cluster to reap gke-backed runtime rows (and vice
	// versa). The client authenticates with the process's Google credentials
	// (workload identity in-cluster, ADC locally) against the separate cluster.
	if endpoint := c.String("assistant-runtime-gke-cluster-endpoint"); endpoint != "" {
		caCert, err := base64.StdEncoding.DecodeString(c.String("assistant-runtime-gke-cluster-ca"))
		if err != nil {
			return nil, fmt.Errorf("decode --assistant-runtime-gke-cluster-ca: %w", err)
		}
		dynamicClient, err := k8s.NewRemoteDynamicClient(ctx, endpoint, caCert)
		if err != nil {
			return nil, fmt.Errorf("build assistant cluster client: %w", err)
		}
		cfg.GKE.Dynamic = dynamicClient
	}

	// Fly and GKE call back to the server at the same URL; validate it once.
	if err := guardianPolicy.ValidateHost(ctx, cfg.Fly.ServerURL.Hostname()); err != nil {
		return nil, fmt.Errorf("assistant runtime requires a public --assistant-runtime-server-url or --server-url; got %q: %w", cfg.Fly.ServerURL.String(), err)
	}

	backend, err := assistants.NewRuntimeBackend(logger, tracerProvider, guardianPolicy, cfg)
	if err != nil {
		return nil, fmt.Errorf("build assistant runtime backend: %w", err)
	}
	return backend, nil
}
