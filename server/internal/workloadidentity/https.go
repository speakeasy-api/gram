package workloadidentity

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// ErrIssuerURLNotHTTPS reports an issuer identifier that is well formed but
// not https. SEP-1933 requires authorization servers to ensure issuer URLs use
// HTTPS, so a plain-http issuer is never a workload issuer, whatever a row
// says. Always reported alongside ErrIssuerURLInvalid.
var ErrIssuerURLNotHTTPS = errors.New("workload issuer url must use https")

// ErrJWKSURINotHTTPS reports a jwks_uri that is not https. SEP-1933 requires
// the jwks_uri to use the https scheme. Distinct from every other key source
// failure so the grant can log this refusal by name: a stored row carrying it
// is a configuration error an operator has to fix, not a bad assertion.
var ErrJWKSURINotHTTPS = errors.New("workload issuer jwks_uri must use https")

// ParseIssuerURL validates raw as a workload issuer identifier and returns its
// canonical form.
//
// Stricter than issuerurl.Parse, which accepts http for its other callers.
// Every failure wraps ErrIssuerURLInvalid; a plain-http URL also wraps
// ErrIssuerURLNotHTTPS.
func ParseIssuerURL(raw string) (issuerurl.Canonical, error) {
	canonical, err := issuerurl.Parse(raw)
	if err != nil {
		return issuerurl.Canonical{}, fmt.Errorf("%w: %w", ErrIssuerURLInvalid, err)
	}

	if canonical.Scheme() != "https" {
		return issuerurl.Canonical{}, fmt.Errorf("%w: %w", ErrIssuerURLInvalid, ErrIssuerURLNotHTTPS)
	}

	return canonical, nil
}

// ValidateJWKSURI reports whether raw may serve as a workload issuer's key set
// URL.
//
// The scheme is checked first and reported as ErrJWKSURINotHTTPS, so a
// plain-http value is named as such rather than as generic junk. Everything
// else, including an empty value, follows jwks.ValidateURI, the rule every
// remote key source applies.
func ValidateJWKSURI(raw string) error {
	if parsed, err := url.Parse(raw); raw != "" && err == nil && parsed.Scheme != "https" {
		return ErrJWKSURINotHTTPS
	}

	if err := jwks.ValidateURI(raw); err != nil {
		return fmt.Errorf("validate jwks_uri: %w", err)
	}

	return nil
}
