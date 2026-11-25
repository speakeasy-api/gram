package gram

import (
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/urfave/cli/v2"
)

// internalIssuerSignerConfig pins signing to this deployment's public origin
// and KMS ring, so a preview database cannot use another deployment's keys.
func internalIssuerSignerConfig(c *cli.Context, serverURL *url.URL) internalsessionissuers.SignerConfig {
	ring := strings.TrimSpace(c.String(identityProviderKMSKeyRingFlag))
	if ring == "" && c.String("environment") == "local" {
		ring = identityProviderKMSKeyRingLocalDefault
	}
	return internalsessionissuers.SignerConfig{
		Deployment:    internalsessionissuers.Deployment{ServerURL: serverURL, AllowLoopbackIssuer: c.String("environment") == "local", KeyRing: ring},
		ManagedSigner: c.String(identityProviderSigningServiceAccount),
	}
}
