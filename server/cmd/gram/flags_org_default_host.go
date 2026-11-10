package gram

import (
	"fmt"
	"net/url"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/orghost"
)

const (
	legacyDefaultHostFlag = "legacy-default-host"
	newOrgDefaultHostFlag = "new-org-default-host"
	platformHostsFlag     = "platform-hosts"
)

// platformHostsCLIFlag lists the extra platform hosts. The server and the
// worker both read it: the worker re-checks organizations' default hosts
// against it.
func platformHostsCLIFlag() cli.Flag {
	return &cli.StringSliceFlag{
		Name:    platformHostsFlag,
		Usage:   "First-party hosts that serve the full product, e.g. app.getgram.ai,ai.speakeasy.com. The server URL's host is always included. Login on the other hosts completes on the same host.",
		EnvVars: []string{"GRAM_PLATFORM_HOSTS"},
	}
}

// orgDefaultHostFlags configure the host of URLs rendered for an organization
// without a request: emails, Slack messages, and background jobs.
func orgDefaultHostFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    legacyDefaultHostFlag,
			Usage:   "Origin of request-less URLs (emails, Slack messages) for organizations with no recorded default host, e.g. https://app.getgram.ai. Keep it fixed when the canonical host moves. Defaults to the site URL.",
			EnvVars: []string{"GRAM_LEGACY_DEFAULT_HOST"},
		},
		&cli.StringFlag{
			Name:    newOrgDefaultHostFlag,
			Usage:   "Origin recorded as the default host of newly created organizations, e.g. https://ai.speakeasy.com. Empty records none, so new organizations use the legacy default host.",
			EnvVars: []string{"GRAM_NEW_ORG_DEFAULT_HOST"},
		},
	}
}

// orgHostResolverFromCLI parses the organization default host flags. Both must
// be origins served by this deployment: the server URL's host or a platform
// host. siteURL is nil when the process has no dashboard URL configured.
func orgHostResolverFromCLI(c *cli.Context, serverURL, siteURL *url.URL, environment string, platformHosts map[string]string) (*orghost.Resolver, error) {
	cfg := orghost.Config{
		ServerURL:                  serverURL,
		SiteURL:                    siteURL,
		PlatformHosts:              platformHosts,
		LegacyDefaultHost:          nil,
		NewOrganizationDefaultHost: nil,
	}
	if raw := c.String(legacyDefaultHostFlag); raw != "" {
		legacy, err := parseCallbackOrigin(raw, serverURL, environment, platformHosts)
		if err != nil {
			return nil, fmt.Errorf("invalid --%s: %w", legacyDefaultHostFlag, err)
		}
		cfg.LegacyDefaultHost = legacy
	}
	if raw := c.String(newOrgDefaultHostFlag); raw != "" {
		newOrg, err := parseCallbackOrigin(raw, serverURL, environment, platformHosts)
		if err != nil {
			return nil, fmt.Errorf("invalid --%s: %w", newOrgDefaultHostFlag, err)
		}
		cfg.NewOrganizationDefaultHost = newOrg
	}
	return orghost.New(cfg), nil
}
