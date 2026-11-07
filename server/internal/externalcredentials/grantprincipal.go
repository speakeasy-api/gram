package externalcredentials

import (
	"errors"
	"fmt"
	"strings"
)

// IAM member prefixes a customer grant principal may use. A group fronts
// Speakeasy's service account so the account can rotate without customers
// changing their grants; a bare service account is accepted for environments
// without one.
const (
	grantMemberGroupPrefix          = "group:"
	grantMemberServiceAccountPrefix = "serviceAccount:"
)

// ErrInvalidGrantPrincipal means a configured customer grant principal is not
// a group: or serviceAccount: IAM member.
var ErrInvalidGrantPrincipal = errors.New("invalid customer grant principal")

// ParseCustomerGrantPrincipal validates the configured IAM member customers
// grant on their service accounts and returns it normalized. An empty value
// returns "", meaning the ambient identity is reported instead.
func ParseCustomerGrantPrincipal(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	for _, prefix := range []string{grantMemberGroupPrefix, grantMemberServiceAccountPrefix} {
		email, ok := strings.CutPrefix(value, prefix)
		if !ok {
			continue
		}
		local, domain, found := strings.Cut(email, "@")
		if !found || local == "" || domain == "" || strings.ContainsAny(email, " \t:") {
			return "", fmt.Errorf("%w: %q has no valid email after %q", ErrInvalidGrantPrincipal, value, prefix)
		}
		return prefix + strings.ToLower(email), nil
	}

	return "", fmt.Errorf("%w: %q must start with %q or %q", ErrInvalidGrantPrincipal, value, grantMemberGroupPrefix, grantMemberServiceAccountPrefix)
}

// grantPrincipalMember is the member customers grant: the configured one, or
// Speakeasy's ambient service account when none is configured.
func grantPrincipalMember(configured, ambientServiceAccount string) string {
	if configured != "" {
		return configured
	}
	if ambientServiceAccount == "" {
		return ""
	}

	return grantMemberServiceAccountPrefix + ambientServiceAccount
}
