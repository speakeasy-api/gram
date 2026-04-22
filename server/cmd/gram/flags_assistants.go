package gram

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/superfly/fly-go/tokens"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/assistants"
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
		Name:    "assistant-runtime-oci-image",
		Usage:   "The OCI image repository for the assistant runtime image. It must not include a tag.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_OCI_IMAGE"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-image-version",
		Usage:   "The assistant runtime image tag/version to run on fly.io.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_IMAGE_VERSION"},
	},
	&cli.StringFlag{
		Name:    "assistant-runtime-flyio-app-name-prefix",
		Usage:   "Prefix for fly.io assistant runtime app names.",
		EnvVars: []string{"GRAM_ASSISTANT_RUNTIME_FLYIO_APP_NAME_PREFIX"},
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
	}
	functionsProvider := c.String("functions-provider")
	functionsRunnerImage := c.String("functions-runner-oci-image")
	if !c.IsSet("assistant-runtime-provider") &&
		previewAssistantRuntimeFallbackEnabled(c.String("server-url"), functionsProvider, functionsRunnerImage) {
		provider = assistants.RuntimeProviderFlyIO
	}

	flyToken := firstNonEmpty(
		c.String("assistant-runtime-flyio-api-token"),
		c.String("functions-flyio-api-token"),
	)
	flyOrg := firstNonEmpty(
		c.String("assistant-runtime-flyio-org"),
		c.String("functions-flyio-org"),
	)
	flyRegion := firstNonEmpty(
		c.String("assistant-runtime-flyio-region"),
		c.String("functions-flyio-region"),
	)
	imageVersion := firstNonEmpty(
		c.String("assistant-runtime-image-version"),
		strings.TrimPrefix(c.String("functions-runner-version"), "sha-"),
		GitSHA,
	)
	assistantOCIImage := firstNonEmpty(
		c.String("assistant-runtime-oci-image"),
		previewAssistantRuntimeOCIImage(c.String("server-url"), functionsRunnerImage),
	)

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
			FlyTokens:          tokens.Parse(flyToken),
			FlyAPIURL:          "",
			FlyMachinesBaseURL: "",
			DefaultFlyOrg:      flyOrg,
			DefaultFlyRegion:   flyRegion,
			OCIImage:           assistantOCIImage,
			ImageVersion:       imageVersion,
			AppNamePrefix:      c.String("assistant-runtime-flyio-app-name-prefix"),
			ServerURLOverride:  override,
		},
	}, nil
}

// Temporary preview-only fallback until gram-infra ships the assistant runtime
// env wiring. PR previews already carry the functions Fly config we need.
func previewAssistantRuntimeFallbackEnabled(rawServerURL string, functionsProvider string, functionsRunnerImage string) bool {
	return previewAssistantRuntimeOCIImage(rawServerURL, functionsRunnerImage) != "" &&
		strings.TrimSpace(functionsProvider) == "flyio"
}

func previewAssistantRuntimeOCIImage(rawServerURL string, functionsRunnerImage string) string {
	if strings.TrimSpace(functionsRunnerImage) == "" {
		return ""
	}
	parsed, err := url.Parse(rawServerURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if !strings.HasPrefix(host, "pr-") || !strings.Contains(host, ".getgram.ai") {
		return ""
	}
	return strings.Replace(functionsRunnerImage, "/gfr-", "/gar-", 1)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
