package gram

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/urfave/cli/v2"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
)

const (
	networkIngressMutationFlag = "network-ingress-provider-mutations-enabled"
	networkIngressQueueFlag    = "network-ingress-reconcile-task-queue"
	networkIngressDevIDFlag    = "network-ingress-dev-id"

	networkIngressGatewayNameQualifierFlag = "network-ingress-gateway-name-qualifier"
)

var networkIngressQueuePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// localDevelopmentNamer returns the namer for a process that changes or hosts
// artifacts on a tailnet. In local development it insists on a development
// id: without one the stack would use production's names, and could overwrite,
// host or delete production's services on a tailnet both serve.
func localDevelopmentNamer(environment, devID, qualifier string) (networkingress.Namer, error) {
	if environment == "local" && devID == "" {
		return nil, errors.New("local development needs GRAM_NETWORK_INGRESS_DEV_ID; run `mise run zero:network-ingress`")
	}
	namer, err := networkingress.NewNamer(environment, devID, qualifier)
	if err != nil {
		return nil, fmt.Errorf("network ingress naming: %w", err)
	}
	return namer, nil
}

// networkIngressGatewayNameQualifierFlagDef is shared by the reconciler, which
// names auth keys and services with it, and the gateway, which names its
// machines with it; both must carry the same value.
func networkIngressGatewayNameQualifierFlagDef() cli.Flag {
	return &cli.StringFlag{
		Name:    networkIngressGatewayNameQualifierFlag,
		Usage:   "Names this environment's gateway machines speakeasy-gw-<qualifier>-<slot> so they stay apart from another environment's on a shared tailnet; empty keeps speakeasy-gw-<slot>",
		EnvVars: []string{"GRAM_NETWORK_INGRESS_GATEWAY_NAME_QUALIFIER"},
	}
}

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
			Usage:   "Allow private ingress provider apply and rotation; set only where tailnet gateway pods run. Observation and cleanup remain independent",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_PROVIDER_MUTATIONS_ENABLED"},
			Value:   false,
		},
		&cli.StringFlag{
			Name: "network-ingress-gateway-tag", Usage: "Tailscale tag carried by gateway auth keys and services",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_GATEWAY_TAG"}, Value: networkingress.DefaultGatewayTag, Action: validateNetworkIngressTag,
		},
		&cli.IntFlag{
			Name: "network-ingress-gateway-slots", Usage: "Node identities (gateway pod ordinals) hosting each ingress",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_GATEWAY_SLOTS"}, Value: tailnetgateway.DefaultSlots,
		},
		&cli.StringFlag{
			Name: "network-ingress-tailscale-api-url", Usage: "Tailscale API base URL override for tests",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_TAILSCALE_API_URL"},
		},
		&cli.StringFlag{
			Name: networkIngressDevIDFlag, Usage: "Local development only: names this stack's hostnames, services and devices local-<id>-... so they stay apart from production's on a shared tailnet",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_DEV_ID"},
		},
		networkIngressGatewayNameQualifierFlagDef(),
	}
}

func networkIngressConfigFromCLI(c *cli.Context) (networkingress.RuntimeConfig, error) {
	var config networkingress.RuntimeConfig
	namer, err := networkingress.NewNamer(c.String("environment"), c.String(networkIngressDevIDFlag), c.String(networkIngressGatewayNameQualifierFlag))
	if err != nil {
		return config, fmt.Errorf("network ingress naming: %w", err)
	}
	config = networkingress.RuntimeConfig{
		ReconcileTaskQueue:       c.String(networkIngressQueueFlag),
		ProviderMutationsEnabled: c.Bool(networkIngressMutationFlag),
		Gateway: networkingress.GatewayRuntimeConfig{
			Tag:    c.String("network-ingress-gateway-tag"),
			Slots:  int32(min(max(c.Int("network-ingress-gateway-slots"), -1), 1<<15)), // #nosec G115 -- clamped before conversion; -1 keeps negatives rejected by Validate.
			APIURL: c.String("network-ingress-tailscale-api-url"),

			Namer: namer,
		},
	}
	if err := config.Validate(); err != nil {
		return config, fmt.Errorf("validate network ingress configuration: %w", err)
	}
	return config, nil
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
