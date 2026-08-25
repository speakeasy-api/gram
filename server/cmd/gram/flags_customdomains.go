package gram

import (
	"fmt"
	"net/netip"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
)

// customDomainFlags are shared by the server and worker commands.
func customDomainFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "custom-domain-k8s-namespace",
			Usage:   "Kubernetes namespace for custom domain ingresses (defaults to gram-<environment>)",
			EnvVars: []string{"GRAM_CUSTOM_DOMAIN_K8S_NAMESPACE"},
		},
		&cli.StringFlag{
			Name:    "custom-domain-backend-service",
			Usage:   "Kubernetes service that custom domain ingresses route to (defaults to gram-server)",
			EnvVars: []string{"GRAM_CUSTOM_DOMAIN_BACKEND_SERVICE"},
		},
		&cli.StringFlag{
			Name:    "custom-domain-cname",
			Usage:   "The expected CNAME target for custom domain verification (e.g., cname.getgram.ai.)",
			EnvVars: []string{"GRAM_CUSTOM_DOMAIN_CNAME"},
		},
		&cli.StringSliceFlag{
			Name:    "custom-domain-a-records",
			Usage:   "The static ingress IPv4 addresses apex custom domains should point A records at (e.g., 34.127.46.134)",
			EnvVars: []string{"GRAM_CUSTOM_DOMAIN_A_RECORDS"},
			Action: func(_ *cli.Context, values []string) error {
				if _, err := customdomains.ParseExpectedARecords(values); err != nil {
					return fmt.Errorf("invalid --custom-domain-a-records: %w", err)
				}
				return nil
			},
		},
	}
}

// customDomainARecordsFromCLI returns the parsed apex A-record targets. The
// flag-level validator has already rejected invalid values, so an error here
// is unreachable in practice but still propagated.
func customDomainARecordsFromCLI(c *cli.Context) ([]netip.Addr, error) {
	addrs, err := customdomains.ParseExpectedARecords(c.StringSlice("custom-domain-a-records"))
	if err != nil {
		return nil, fmt.Errorf("invalid --custom-domain-a-records: %w", err)
	}
	return addrs, nil
}
