package gram

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/urfave/cli/v2"
)

const (
	// codeClusterQPS allows the bounded pool to admit runs without the client's 5 QPS default.
	codeClusterQPS = 100
	// codeClusterBurst covers simultaneous ownership and reconnect checks across 16 pods.
	codeClusterBurst = 200
)

func newCodeExecutor(c *cli.Context, logger *slog.Logger, policy *guardian.Policy, ownership codemode.OwnershipStore) (codemode.Executor, error) {
	switch c.String("code-runtime-provider") {
	case "", "disabled":
		return codemode.Disabled{}, nil
	case "local":
		provider, err := newLocalCodeProvider(c, policy)
		if err != nil {
			return nil, err
		}
		coordinator, err := codemode.NewCoordinator(provider, ownership, codeRuntimeLimits(c))
		if err != nil {
			_ = provider.Runner.Close()
			return nil, fmt.Errorf("configure code coordinator: %w", err)
		}
		return coordinator, nil
	case "gke":
		provider, err := newGKECodeProvider(c, logger, policy)
		if err != nil {
			return nil, err
		}
		coordinator, err := codemode.NewCoordinator(provider, ownership, codeRuntimeLimits(c))
		if err != nil {
			_ = provider.Shutdown(c.Context)
			return nil, fmt.Errorf("configure code coordinator: %w", err)
		}
		return coordinator, nil
	default:
		return nil, fmt.Errorf("unsupported code runtime provider")
	}
}

// codeRuntimeLimits reads the per-replica execution limits; zero values use
// the coordinator's defaults.
func codeRuntimeLimits(c *cli.Context) codemode.Limits {
	return codemode.Limits{
		OrganizationExecutions: c.Int("code-runtime-max-organization-executions"),
		CallerExecutions:       c.Int("code-runtime-max-caller-executions"),
		AdmissionWait:          c.Duration("code-runtime-admission-wait"),
	}
}

func newLocalCodeProvider(c *cli.Context, policy *guardian.Policy) (*codemode.StaticProvider, error) {
	if c.String("environment") != "local" {
		return nil, fmt.Errorf("local code runtime requires the local environment")
	}
	u, err := url.Parse(c.String("code-runtime-url"))
	if err != nil {
		return nil, fmt.Errorf("parse local code runtime URL: %w", err)
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("local code runtime requires a literal loopback IP")
	}
	bits := net.IPv6len * 8
	if ip.To4() != nil {
		bits = net.IPv4len * 8
	}
	allow := &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
	runner, err := codemode.NewRunnerClient(u.String(), c.String("code-runtime-token"), policy.Dialer(guardian.WithDialerAllowedCIDRBlocks([]*net.IPNet{allow})))
	if err != nil {
		return nil, fmt.Errorf("configure local code runtime: %w", err)
	}
	context.AfterFunc(c.Context, func() { _ = runner.Close() })
	return &codemode.StaticProvider{Runner: runner}, nil
}

func newGKECodeProvider(c *cli.Context, logger *slog.Logger, policy *guardian.Policy) (*codemode.GKEProvider, error) {
	token, err := codeRuntimeToken(c)
	if err != nil {
		return nil, err
	}
	assistantNamespace := c.String("assistant-runtime-gke-namespace")
	if assistantNamespace == "" {
		assistantNamespace = "gram-" + c.String("environment")
	}
	if c.String("code-runtime-gke-namespace") == assistantNamespace {
		return nil, fmt.Errorf("code and assistant runtimes require separate namespaces")
	}
	endpoint := c.String("code-runtime-gke-cluster-endpoint")
	if strings.Contains(endpoint, "://") && !strings.HasPrefix(endpoint, "https://") {
		return nil, fmt.Errorf("code runtime cluster endpoint must use HTTPS")
	}
	if c.Int("code-runtime-max-runners") < 1 || c.Int("code-runtime-max-organization-runners") < 1 {
		return nil, fmt.Errorf("code runtime runner limits must be positive")
	}
	ca, err := base64.StdEncoding.DecodeString(c.String("code-runtime-gke-cluster-ca"))
	if err != nil {
		return nil, fmt.Errorf("decode code runtime cluster CA: %w", err)
	}
	client, err := k8s.NewRemoteDynamicClient(c.Context, endpoint, ca, k8s.RemoteClientOptions{QPS: codeClusterQPS, Burst: codeClusterBurst})
	if err != nil {
		return nil, fmt.Errorf("configure code runtime cluster: %w", err)
	}
	cidrs := make([]*net.IPNet, 0, len(c.StringSlice("code-runtime-gke-runner-cidr")))
	for _, value := range c.StringSlice("code-runtime-gke-runner-cidr") {
		_, cidr, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("parse code runtime pod CIDR: %w", err)
		}
		cidrs = append(cidrs, cidr)
	}
	provider, err := codemode.NewGKEProvider(c.Context, codemode.GKEConfig{
		Dynamic: client, Namespace: c.String("code-runtime-gke-namespace"), Template: c.String("code-runtime-gke-sandbox-template"),
		Image: c.String("code-runtime-image"), AllowTaggedImage: c.Bool("code-runtime-is-preview"), Port: c.Int("code-runtime-port"), Token: token,
		Dialer: policy.Dialer(guardian.WithDialerAllowedCIDRBlocks(cidrs)), CIDRs: cidrs, Logger: logger,
		MaxRunners: c.Int("code-runtime-max-runners"), MaxOrganizationRunners: c.Int("code-runtime-max-organization-runners"),
	})
	if err != nil {
		return nil, fmt.Errorf("configure code sandbox provider: %w", err)
	}
	return provider, nil
}

// codeRuntimeToken keeps preview credentials separate even when previews use
// the development cluster and workload identity.
func codeRuntimeToken(c *cli.Context) (string, error) {
	token := c.String("code-runtime-token")
	if c.Bool("code-runtime-is-preview") {
		preview := c.String("code-runtime-preview-token")
		if preview == "" || preview == token {
			return "", fmt.Errorf("code runtime previews require a distinct preview token")
		}
		token = preview
	}
	return token, nil
}

func codeRuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "code-runtime-provider", Usage: "Code runtime provider (disabled, local or gke)", EnvVars: []string{"GRAM_CODE_RUNTIME_PROVIDER"}, Value: "disabled"},
		&cli.StringFlag{Name: "code-runtime-url", Usage: "Operator-owned local code runner URL", EnvVars: []string{"GRAM_CODE_RUNTIME_URL"}},
		&cli.StringFlag{Name: "code-runtime-token", Usage: "Authentication token for the dedicated code runtime", EnvVars: []string{"GRAM_CODE_RUNTIME_TOKEN"}},
		&cli.BoolFlag{Name: "code-runtime-is-preview", Usage: "Require isolated preview code runtime credentials and allow a tagged runner image", EnvVars: []string{"GRAM_IS_PREVIEW"}},
		&cli.StringFlag{Name: "code-runtime-preview-token", Usage: "Separate authentication token for preview code runtimes", EnvVars: []string{"GRAM_CODE_RUNTIME_PREVIEW_TOKEN"}},
		&cli.StringFlag{Name: "code-runtime-image", Usage: "Code runner image reference pinned with @sha256: (previews may use a tag)", EnvVars: []string{"GRAM_CODE_RUNTIME_IMAGE"}},
		&cli.IntFlag{Name: "code-runtime-port", Usage: "Code runner pod HTTP port", EnvVars: []string{"GRAM_CODE_RUNTIME_PORT"}, Value: 8081},
		&cli.StringFlag{Name: "code-runtime-gke-cluster-endpoint", Usage: "GKE code sandbox cluster API endpoint", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_CLUSTER_ENDPOINT"}},
		&cli.StringFlag{Name: "code-runtime-gke-cluster-ca", Usage: "Base64-encoded GKE code sandbox cluster CA", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_CLUSTER_CA"}},
		&cli.StringFlag{Name: "code-runtime-gke-namespace", Usage: "Dedicated code sandbox namespace", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_NAMESPACE"}},
		&cli.StringFlag{Name: "code-runtime-gke-sandbox-template", Usage: "Dedicated code SandboxTemplate", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_SANDBOX_TEMPLATE"}},
		&cli.StringSliceFlag{Name: "code-runtime-gke-runner-cidr", Usage: "Permitted code sandbox pod CIDRs", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_RUNNER_CIDR"}},
		&cli.IntFlag{Name: "code-runtime-max-runners", Usage: "Most code runner pods one replica claims across organizations", EnvVars: []string{"GRAM_CODE_RUNTIME_MAX_RUNNERS"}, Value: 64},
		&cli.IntFlag{Name: "code-runtime-max-organization-runners", Usage: "Most shared code runner pods one organization holds on a replica", EnvVars: []string{"GRAM_CODE_RUNTIME_MAX_ORGANIZATION_RUNNERS"}, Value: 4},
		&cli.IntFlag{Name: "code-runtime-max-organization-executions", Usage: "Most concurrent code executions one organization runs on a replica (0 uses the default)", EnvVars: []string{"GRAM_CODE_RUNTIME_MAX_ORGANIZATION_EXECUTIONS"}, Value: 0},
		&cli.IntFlag{Name: "code-runtime-max-caller-executions", Usage: "Most concurrent code executions one caller runs on a replica (0 uses the default)", EnvVars: []string{"GRAM_CODE_RUNTIME_MAX_CALLER_EXECUTIONS"}, Value: 0},
		&cli.DurationFlag{Name: "code-runtime-admission-wait", Usage: "How long a code execution waits for a free slot before a retryable refusal (0 uses the default)", EnvVars: []string{"GRAM_CODE_RUNTIME_ADMISSION_WAIT"}, Value: 0},
	}
}
