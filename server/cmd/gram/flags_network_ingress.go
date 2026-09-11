package gram

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/urfave/cli/v2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
)

const (
	networkIngressMutationFlag = "network-ingress-provider-mutations-enabled"
	networkIngressQueueFlag    = "network-ingress-reconcile-task-queue"
)

var networkIngressQueuePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// The reconciliation queue is explicit: preview processes must not implicitly
// claim production lifecycle work through their general-purpose task queue.
func networkIngressQueueFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    networkIngressQueueFlag,
			Usage:   "Authoritative private ingress reconciliation task queue; empty disables lifecycle delivery",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_RECONCILE_TASK_QUEUE"},
			Action: func(_ *cli.Context, value string) error {
				if value != "" && !networkIngressQueuePattern.MatchString(value) {
					return fmt.Errorf("invalid private ingress reconciliation task queue")
				}
				return nil
			},
		},
	}
}

func networkIngressProviderFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:    networkIngressMutationFlag,
			Usage:   "Allow private ingress provider apply and rotation; observation and cleanup remain independent",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_PROVIDER_MUTATIONS_ENABLED"},
			Value:   false,
		},
		&cli.StringFlag{
			Name: "network-ingress-operator-namespace", Usage: "Namespace containing the Tailscale operator and OAuth Secrets",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_OPERATOR_NAMESPACE"}, Action: validateNetworkIngressName,
		},
		&cli.StringFlag{
			Name: "network-ingress-backend-namespace", Usage: "Namespace containing the private backend Service",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_BACKEND_NAMESPACE"}, Action: validateNetworkIngressName,
		},
		&cli.StringFlag{
			Name: "network-ingress-backend-service", Usage: "Private backend Kubernetes Service name",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_BACKEND_SERVICE"}, Action: validateNetworkIngressName,
		},
		&cli.IntFlag{
			Name: "network-ingress-backend-port", Usage: "Private backend HTTPS Service port",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_BACKEND_PORT"}, Action: validateNetworkIngressPort,
		},
		&cli.StringFlag{
			Name: "network-ingress-attestor-image", Usage: "Attestor image pinned by sha256 digest",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_ATTESTOR_IMAGE"},
			Action: func(_ *cli.Context, value string) error {
				if value == "" {
					return nil
				}
				return networkingress.ValidateAttestorImageReference(value)
			},
		},
		&cli.StringFlag{
			Name: "network-ingress-attestor-ca-secret", Usage: "CA Secret in the backend namespace for attestor TLS trust",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_ATTESTOR_CA_SECRET"}, Action: validateNetworkIngressName,
		},
		&cli.StringFlag{
			Name: "network-ingress-backend-pod-labels", Usage: "Equality-based pod labels selecting the private backend",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_BACKEND_POD_LABELS"},
			Action: func(_ *cli.Context, value string) error {
				if value == "" {
					return nil
				}
				if _, err := labels.ConvertSelectorToLabelsMap(value); err != nil {
					return fmt.Errorf("invalid private ingress backend pod labels")
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name: "network-ingress-proxy-tag", Usage: "Tailscale tag assigned to ingress proxies",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_PROXY_TAG"}, Action: validateNetworkIngressTag,
		},
		&cli.StringFlag{
			Name: "network-ingress-service-tag", Usage: "Tailscale tag assigned to ingress services",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_SERVICE_TAG"}, Action: validateNetworkIngressTag,
		},
		&cli.StringFlag{
			Name: "network-ingress-class", Usage: "Private ingress class; only tailscale is supported",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_CLASS"},
			Action: func(_ *cli.Context, value string) error {
				if value != "" && value != "tailscale" {
					return fmt.Errorf("private ingress class must be tailscale")
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name: "network-ingress-token-audience", Usage: "Projected token audience; must match the private listener",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_TOKEN_AUDIENCE"},
			Action: func(_ *cli.Context, value string) error {
				if value != "" && value != "gram-netingress" {
					return fmt.Errorf("private ingress token audience must be gram-netingress")
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name: "network-ingress-kubernetes-api-cidr", Usage: "Kubernetes API destination CIDR for private ingress NetworkPolicies",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_KUBERNETES_API_CIDR"}, Action: validateNetworkIngressCIDR,
		},
		&cli.IntFlag{
			Name: "network-ingress-kubernetes-api-port", Usage: "Kubernetes API destination port for private ingress NetworkPolicies",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_KUBERNETES_API_PORT"}, Action: validateNetworkIngressPort,
		},
		&cli.StringSliceFlag{
			Name: "network-ingress-cluster-cidrs", Usage: "Cluster destination CIDRs excluded from proxy internet egress",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_CLUSTER_CIDRS"},
			Action: func(c *cli.Context, values []string) error {
				for _, value := range values {
					if value == "" {
						return fmt.Errorf("private ingress cluster CIDRs must not contain empty entries")
					}
					if err := validateNetworkIngressCIDR(c, value); err != nil {
						return err
					}
				}
				return nil
			},
		},
	}
}

func networkIngressConfigFromCLI(c *cli.Context) (networkingress.RuntimeConfig, error) {
	var config networkingress.RuntimeConfig
	backendPort := c.Int("network-ingress-backend-port")
	if err := validateNetworkIngressPort(c, backendPort); err != nil {
		return config, err
	}
	apiPort := c.Int("network-ingress-kubernetes-api-port")
	if err := validateNetworkIngressPort(c, apiPort); err != nil {
		return config, err
	}
	backendLabels, err := labels.ConvertSelectorToLabelsMap(c.String("network-ingress-backend-pod-labels"))
	if err != nil {
		return config, fmt.Errorf("invalid private ingress backend pod labels")
	}
	config = networkingress.RuntimeConfig{
		ReconcileTaskQueue:       c.String(networkIngressQueueFlag),
		ProviderMutationsEnabled: c.Bool(networkIngressMutationFlag),
		Tailscale: k8s.TailscaleNetworkIngressConfig{
			OperatorNamespace: c.String("network-ingress-operator-namespace"),
			BackendNamespace:  c.String("network-ingress-backend-namespace"),
			BackendPodLabels:  backendLabels,
			ProxyTag:          c.String("network-ingress-proxy-tag"),
			ServiceTag:        c.String("network-ingress-service-tag"),
			AttestorCASecret:  c.String("network-ingress-attestor-ca-secret"),
			KubernetesAPICIDR: c.String("network-ingress-kubernetes-api-cidr"),
			KubernetesAPIPort: int32(apiPort), // #nosec G115 -- validated port range before conversion.
			ClusterCIDRs:      append([]string(nil), c.StringSlice("network-ingress-cluster-cidrs")...),
		},
		AttestorImage:  c.String("network-ingress-attestor-image"),
		BackendService: c.String("network-ingress-backend-service"),
		BackendPort:    int32(backendPort), // #nosec G115 -- validated port range before conversion.
		IngressClass:   c.String("network-ingress-class"),
		TokenAudience:  c.String("network-ingress-token-audience"),
	}
	if err := config.Validate(); err != nil {
		return config, fmt.Errorf("validate network ingress configuration: %w", err)
	}
	return config, nil
}

func validateNetworkIngressName(_ *cli.Context, value string) error {
	if value != "" && len(validation.IsDNS1123Label(value)) > 0 {
		return fmt.Errorf("invalid private ingress Kubernetes name")
	}
	return nil
}

func validateNetworkIngressCIDR(_ *cli.Context, value string) error {
	if value == "" {
		return nil
	}
	if _, err := netip.ParsePrefix(value); err != nil {
		return fmt.Errorf("invalid private ingress network CIDR")
	}
	return nil
}

func validateNetworkIngressPort(_ *cli.Context, value int) error {
	if value < 0 || value > 65535 {
		return fmt.Errorf("private ingress port must be between 1 and 65535, or zero when unconfigured")
	}
	return nil
}

func validateNetworkIngressTag(_ *cli.Context, value string) error {
	if value == "" {
		return nil
	}
	tag, ok := strings.CutPrefix(value, "tag:")
	if !ok || tag == "" || len(validation.IsDNS1123Label(tag)) > 0 {
		return fmt.Errorf("invalid private ingress Tailscale tag")
	}
	return nil
}
