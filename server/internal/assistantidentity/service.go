package assistantidentity

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/tunnel/jwks"
)

// Service registers assistant trigger workloads under the deployment's Gram
// signing issuer through ordinary project trust registrations. It neither
// mints credentials nor treats issuer URL equality as tenant identity.
type Service struct {
	issuer  string
	jwksURI string

	// issuerSpellings are the stored issuer values that name the same origin,
	// so an existing registration is reused however it was spelled.
	issuerSpellings []string

	audit *audit.Logger
}

// New expects an issuer origin already checked with mcpauthz.ValidateIssuerOrigin,
// the same origin the caller-assertion signer uses.
func New(issuerURL string, auditLogger *audit.Logger) *Service {
	issuer := strings.TrimRight(issuerURL, "/")
	spellings := []string{issuer}
	if canonical, err := issuerurl.Parse(issuer); err == nil {
		spellings = canonical.MatchCandidates()
	}
	return &Service{issuer: issuer, jwksURI: issuer + jwks.Path, issuerSpellings: spellings, audit: auditLogger}
}
