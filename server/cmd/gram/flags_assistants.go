package gram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/superfly/fly-go/tokens"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

var assistantRuntimeFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "assistant-runtime-provider",
		Usage:   "Assistant runtime provider. Allowed values: flyio.",
		Value:   assistants.RuntimeProviderFlyIO,
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_PROVIDER"},
		Action: func(_ *cli.Context, val string) error {
			switch val {
			case "", assistants.RuntimeProviderFlyIO:
				return nil
			default:
				return fmt.Errorf("invalid assistant runtime provider: %s", val)
			}
		},
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
		Name:    "assistant-runtime-image-version",
		Usage:   "The assistant runtime image tag/version to run on fly.io.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_IMAGE_VERSION"},
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
	if provider == "" {
		provider = assistants.RuntimeProviderFlyIO
	}
	if provider != assistants.RuntimeProviderFlyIO {
		return assistants.RuntimeBackendConfig{}, fmt.Errorf("invalid assistant runtime provider: %s", provider)
	}

	return assistants.RuntimeBackendConfig{
		Provider: provider,
		Fly: assistants.FlyRuntimeConfig{
			ServiceName:        "gram",
			ServiceVersion:     GitSHA,
			FlyTokens:          tokens.Parse(c.String("assistant-runtime-flyio-api-token")),
			FlyAPIURL:          "",
			FlyMachinesBaseURL: "",
			DefaultFlyOrg:      c.String("assistant-runtime-flyio-org"),
			DefaultFlyRegion:   c.String("assistant-runtime-flyio-region"),
			OCIImage:           c.String("assistant-runtime-oci-image"),
			ImageVersion:       c.String("assistant-runtime-image-version"),
			AppNamePrefix:      c.String("assistant-runtime-flyio-app-name-prefix"),
			ServerURL:          resolvedServerURL,
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
	if err := cfg.Fly.Validate(); err != nil {
		return nil, fmt.Errorf("invalid fly assistant runtime config: %w", err)
	}
	if err := guardianPolicy.ValidateHost(ctx, cfg.Fly.ServerURL.Hostname()); err != nil {
		return nil, fmt.Errorf("assistant fly runtime requires a public --assistant-runtime-server-url or --server-url; got %q: %w", cfg.Fly.ServerURL.String(), err)
	}
	return assistants.NewRuntimeBackend(logger, tracerProvider, guardianPolicy, cfg), nil
}
