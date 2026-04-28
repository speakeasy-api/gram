package gram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/superfly/fly-go/tokens"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

var assistantRuntimeFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "assistant-runtime-provider",
		Usage:   "Assistant runtime provider. Allowed values: local, flyio.",
		Value:   assistants.RuntimeProviderLocal,
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_PROVIDER"},
		Action: func(_ *cli.Context, val string) error {
			switch val {
			case "", assistants.RuntimeProviderLocal, assistants.RuntimeProviderFlyIO, "firecracker":
				return nil
			default:
				return fmt.Errorf("invalid assistant runtime provider: %s", val)
			}
		},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-firecracker-bin",
		Usage:   "Path to the Firecracker binary used for assistant runtimes.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_FIRECRACKER_BIN"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-kernel-path",
		Usage:   "Path to the guest kernel used for assistant runtimes.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_KERNEL_PATH"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-rootfs-path",
		Usage:   "Path to the guest rootfs ext4 image used for assistant runtimes.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_ROOTFS_PATH"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-workdir",
		Usage:   "Directory where per-thread assistant runtime state is created.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_WORKDIR"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-server-url",
		Usage:   "Optional host-reachable server base URL for assistant runtimes. Defaults to rewriting --server-url when it points at localhost.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_SERVER_URL"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-server-hostname",
		Usage:   "Stable hostname that assistant runtimes should use when calling back into the Gram server.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_SERVER_HOSTNAME"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-server-ip",
		Usage:   "Optional IP address that Firecracker guests should map to the assistant runtime server hostname.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_SERVER_IP"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-host-kind",
		Usage:   "Host strategy for assistant runtimes. Allowed values: linux, lima.",
		Value:   "linux",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_HOST_KIND"},
		Action: func(_ *cli.Context, val string) error {
			switch val {
			case assistants.RuntimeHostKindLinux, assistants.RuntimeHostKindLima:
				return nil
			default:
				return fmt.Errorf("invalid assistant runtime host kind: %s", val)
			}
		},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-lima-instance",
		Usage:   "Lima instance name to use when assistant runtimes are hosted inside Lima on macOS.",
		Value:   "gram-firecracker",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_LIMA_INSTANCE"},
	},
	&cli.IntFlag{
		Name:    "assistant-runtime-guest-port",
		Usage:   "Guest HTTP port exposed by the assistant runtime.",
		Value:   8081,
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_GUEST_PORT"},
	},
	&cli.IntFlag{
		Name:    "assistant-runtime-memory-mib",
		Usage:   "Memory size in MiB for assistant Firecracker VMs.",
		Value:   1024,
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_MEMORY_MIB"},
	},
	&cli.Int64Flag{
		Name:    "assistant-runtime-vcpu-count",
		Usage:   "vCPU count for assistant Firecracker VMs.",
		Value:   2,
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_VCPU_COUNT"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-tap-prefix",
		Usage:   "Prefix used when creating per-runtime tap devices.",
		Value:   "gramast",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_TAP_PREFIX"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-network-base-cidr",
		Usage:   "IPv4 base CIDR used to allocate /30 host/guest subnets for assistant runtimes.",
		Value:   "172.29.0.0/16",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_NETWORK_BASE_CIDR"},
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

func assistantRuntimeConfigFromCLI(c *cli.Context) (assistants.RuntimeBackendConfig, error) {
	var override *url.URL
	if raw := c.String("assistant-runtime-server-url"); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil {
			return assistants.RuntimeBackendConfig{}, fmt.Errorf("parse --assistant-runtime-server-url: %w", err)
		}
		override = parsed
	}

	provider := c.String("assistant-runtime-provider")
	switch provider {
	case "", "firecracker":
		provider = assistants.RuntimeProviderLocal
	case assistants.RuntimeProviderLocal, assistants.RuntimeProviderFlyIO:
	default:
		return assistants.RuntimeBackendConfig{}, fmt.Errorf("invalid assistant runtime provider: %s", provider)
	}

	return assistants.RuntimeBackendConfig{
		Provider: provider,
		Local: assistants.RuntimeManagerConfig{
			FirecrackerBinPath: c.String("assistant-runtime-firecracker-bin"),
			KernelImagePath:    c.String("assistant-runtime-kernel-path"),
			RootFSPath:         c.String("assistant-runtime-rootfs-path"),
			Workdir:            c.String("assistant-runtime-workdir"),
			GuestAPIPort:       c.Int("assistant-runtime-guest-port"),
			MemoryMiB:          c.Int("assistant-runtime-memory-mib"),
			VCPUCount:          c.Int64("assistant-runtime-vcpu-count"),
			TapPrefix:          c.String("assistant-runtime-tap-prefix"),
			NetworkBaseCIDR:    c.String("assistant-runtime-network-base-cidr"),
			ServerURLOverride:  override,
			ServerHostname:     c.String("assistant-runtime-server-hostname"),
			ServerIPOverride:   c.String("assistant-runtime-server-ip"),
			HostKind:           c.String("assistant-runtime-host-kind"),
			LimaInstance:       c.String("assistant-runtime-lima-instance"),
			OnUnexpectedExit:   nil,
		},
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
			ServerURLOverride:  override,
		},
	}, nil
}

// newAssistantRuntime resolves CLI flags into an assistant RuntimeBackend.
// Construction is deferred for any non-local provider: a stub backend is
// returned so the assistants service can mount its CRUD surface without
// touching Lima/Firecracker paths or other host-specific resources that
// only exist in the local development environment. Concrete remote
// providers (flyio) are wired up in their own follow-up PRs.
func newAssistantRuntime(
	ctx context.Context,
	logger *slog.Logger,
	c *cli.Context,
	guardianPolicy *guardian.Policy,
	db *pgxpool.Pool,
	serverURL *url.URL,
) (assistants.RuntimeBackend, error) {
	cfg, err := assistantRuntimeConfigFromCLI(c)
	if err != nil {
		return nil, err
	}
	if cfg.Provider == assistants.RuntimeProviderLocal {
		cfg.Local.OnUnexpectedExit = assistants.NewUnexpectedRuntimeExitHandler(logger, db)
	}
	rb := assistants.NewRuntimeBackend(logger, guardianPolicy, cfg)
	if err := assistants.ValidateRuntimeBackendServerURL(ctx, rb, serverURL); err != nil {
		return nil, fmt.Errorf("validate assistant runtime server url: %w", err)
	}
	return rb, nil
}
