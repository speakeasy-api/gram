package assistantidentity

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/tunnel/jwks"
)

// Service pins ordinary tenant trust registrations to the deployment's single
// Gram signing issuer and existing public RSA key endpoint. It neither mints
// credentials nor treats issuer URL equality as tenant identity.
type Service struct {
	issuer  string
	jwksURI string
	rollout Rollout
}

// New takes the same issuer origin as mcpauthz.New. Local deployments may use
// HTTP; production configuration must use HTTPS. No tenant-specific issuer or
// alternate signing infrastructure is created here.
func New(issuerURL string, allowHTTP bool, rollout ...Rollout) (*Service, error) {
	if len(rollout) > 1 {
		return nil, fmt.Errorf("multiple rollout configurations: %w", ErrInvalidIdentity)
	}
	u, err := url.Parse(issuerURL)
	validOrigin := u != nil && u.Hostname() != "" && !strings.Contains(issuerURL, "#") && u.User == nil && !u.ForceQuery && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
	validScheme := u != nil && (u.Scheme == "https" || (allowHTTP && u.Scheme == "http"))
	if err != nil || !validOrigin || !validScheme {
		return nil, fmt.Errorf("platform workload issuer must be the configured Gram issuer origin: %w", ErrInvalidIdentity)
	}
	issuer := strings.TrimRight(issuerURL, "/")
	var gates Rollout
	if len(rollout) > 0 {
		gates = rollout[0]
	}
	return &Service{issuer: issuer, jwksURI: issuer + jwks.Path, rollout: gates}, nil
}
