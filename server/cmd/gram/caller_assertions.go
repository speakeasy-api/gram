package gram

import (
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/urfave/cli/v2"
)

func newCallerAssertions(c *cli.Context) (*mcpauthz.Issuer, error) {
	privateKey, publicKeys, issuerURL := c.String("authz-private-key"), c.String("authz-public-keys"), c.String("authz-issuer-url")
	if c.Bool("tunnel-gateway-enabled") && (strings.TrimSpace(privateKey) == "" || strings.TrimSpace(publicKeys) == "" || strings.TrimSpace(issuerURL) == "") {
		return nil, errors.New("tunnel gateways require GRAM_AUTHZ_PRIVATE_KEY, GRAM_AUTHZ_PUBLIC_KEYS and GRAM_AUTHZ_ISSUER_URL")
	}
	issuer, err := mcpauthz.New(privateKey, publicKeys, issuerURL, c.String("environment") == "local")
	if err != nil {
		return nil, fmt.Errorf("initialize caller assertion issuer: %w", err)
	}
	return issuer, nil
}
