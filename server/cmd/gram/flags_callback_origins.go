package gram

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const (
	outboundCallbackURLFlag     = "outbound-callback-url"
	registrationCallbackURLFlag = "registration-callback-url"
)

// callbackOriginFlags configure the origins of the redirect_uri and client
// identity URLs Speakeasy registers with upstream OAuth providers.
func callbackOriginFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    outboundCallbackURLFlag,
			Usage:   "Origin of the OAuth redirect_uri, client metadata document, and JWKS URLs for remote session clients with no recorded callback origin. Providers store these URLs, so keep it fixed when the server URL moves. Defaults to the server URL.",
			EnvVars: []string{"GRAM_OUTBOUND_CALLBACK_URL"},
		},
		&cli.StringFlag{
			Name:    registrationCallbackURLFlag,
			Usage:   "Origin recorded on newly created organization-owned remote session clients, e.g. https://ai.speakeasy.com. Empty records none, so new clients use the outbound callback URL.",
			EnvVars: []string{"GRAM_REGISTRATION_CALLBACK_URL"},
		},
	}
}

// callbackOriginsFromCLI parses the callback origin flags. Both must be
// origins served by this deployment: the server URL's host or a platform host.
func callbackOriginsFromCLI(c *cli.Context, serverURL *url.URL, environment string, platformHosts map[string]string) (remotesessions.CallbackOrigins, error) {
	origins := remotesessions.DefaultCallbackOrigins(serverURL)
	if raw := c.String(outboundCallbackURLFlag); raw != "" {
		outbound, err := parseCallbackOrigin(raw, serverURL, environment, platformHosts)
		if err != nil {
			return origins, fmt.Errorf("invalid --%s: %w", outboundCallbackURLFlag, err)
		}
		origins.Outbound = outbound
	}
	if raw := c.String(registrationCallbackURLFlag); raw != "" {
		registration, err := parseCallbackOrigin(raw, serverURL, environment, platformHosts)
		if err != nil {
			return origins, fmt.Errorf("invalid --%s: %w", registrationCallbackURLFlag, err)
		}
		origins.Registration = registration
	}
	return origins, nil
}

func parseCallbackOrigin(raw string, serverURL *url.URL, environment string, platformHosts map[string]string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if err := validateServerURL(parsed, environment); err != nil {
		return nil, err
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("must be an origin without a path")
	}
	host, err := requestorigin.CanonicalHost(parsed.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid host: %w", err)
	}
	serverHost, err := requestorigin.CanonicalHost(serverURL.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid server host: %w", err)
	}
	if _, ok := platformHosts[host]; !ok && host != serverHost {
		return nil, fmt.Errorf("host %s is neither the server URL host nor a platform host", host)
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}, nil
}
