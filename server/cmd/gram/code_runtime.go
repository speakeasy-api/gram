package gram

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/url"

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
		coordinator, err := codemode.NewCoordinator(&codemode.StaticProvider{Runner: runner}, ownership)
		if err != nil {
			_ = runner.Close()
			return nil, fmt.Errorf("configure code coordinator: %w", err)
		}
		return coordinator, nil
	case "gke":
		if namespace := c.String("code-runtime-gke-namespace"); namespace != "" && namespace == c.String("assistant-runtime-gke-namespace") {
			return nil, fmt.Errorf("code and assistant runtimes require separate namespaces")
		}
		ca, err := base64.StdEncoding.DecodeString(c.String("code-runtime-gke-cluster-ca"))
		if err != nil {
			return nil, fmt.Errorf("decode code runtime cluster CA: %w", err)
		}
		client, err := k8s.NewRemoteDynamicClient(c.Context, c.String("code-runtime-gke-cluster-endpoint"), ca, k8s.RemoteClientOptions{QPS: codeClusterQPS, Burst: codeClusterBurst})
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
			Image: c.String("code-runtime-image"), Port: c.Int("code-runtime-port"), Token: c.String("code-runtime-token"),
			Dialer: policy.Dialer(guardian.WithDialerAllowedCIDRBlocks(cidrs)), CIDRs: cidrs, Logger: logger,
		})
		if err != nil {
			return nil, fmt.Errorf("configure code sandbox provider: %w", err)
		}
		coordinator, err := codemode.NewCoordinator(provider, ownership)
		if err != nil {
			return nil, fmt.Errorf("configure code coordinator: %w", err)
		}
		return coordinator, nil

	default:
		return nil, fmt.Errorf("unsupported code runtime provider")
	}
}

func codeRuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "code-runtime-provider", Usage: "Code runtime provider (disabled, local or gke)", EnvVars: []string{"GRAM_CODE_RUNTIME_PROVIDER"}, Value: "disabled"},
		&cli.StringFlag{Name: "code-runtime-url", Usage: "Operator-owned local code runner URL", EnvVars: []string{"GRAM_CODE_RUNTIME_URL"}},
		&cli.StringFlag{Name: "code-runtime-token", Usage: "Authentication token for the dedicated code runtime", EnvVars: []string{"GRAM_CODE_RUNTIME_TOKEN"}},
		&cli.StringFlag{Name: "code-runtime-image", Usage: "Code runner image pinned by SHA-256 digest", EnvVars: []string{"GRAM_CODE_RUNTIME_IMAGE"}},
		&cli.IntFlag{Name: "code-runtime-port", Usage: "Code runner pod HTTP port", EnvVars: []string{"GRAM_CODE_RUNTIME_PORT"}, Value: 8081},
		&cli.StringFlag{Name: "code-runtime-gke-cluster-endpoint", Usage: "GKE code sandbox cluster API endpoint", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_CLUSTER_ENDPOINT"}},
		&cli.StringFlag{Name: "code-runtime-gke-cluster-ca", Usage: "Base64-encoded GKE code sandbox cluster CA", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_CLUSTER_CA"}},
		&cli.StringFlag{Name: "code-runtime-gke-namespace", Usage: "Dedicated code sandbox namespace", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_NAMESPACE"}},
		&cli.StringFlag{Name: "code-runtime-gke-sandbox-template", Usage: "Dedicated code SandboxTemplate", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_SANDBOX_TEMPLATE"}},
		&cli.StringSliceFlag{Name: "code-runtime-gke-runner-cidr", Usage: "Permitted code sandbox pod CIDRs", EnvVars: []string{"GRAM_CODE_RUNTIME_GKE_RUNNER_CIDR"}},
	}
}
