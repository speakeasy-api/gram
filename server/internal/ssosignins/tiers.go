package ssosignins

import (
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/ssosignins/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// WorkOS authentication methods.
const (
	MethodSSO            = "SSO"
	MethodGoogleOAuth    = "GoogleOAuth"
	MethodMicrosoftOAuth = "MicrosoftOAuth"
	MethodMagicAuth      = "MagicAuth"
	MethodPassword       = "Password"
)

// Sign-in tiers an organization can allow to back a signed assertion.
const (
	TierSSO         = "sso"
	TierGoogle      = "google"
	TierMicrosoft   = "microsoft"
	TierMagicLink   = "magic_link"
	TierPasswordMFA = "password_mfa"
)

// AllTiers is every tier, the default when an organization sets none.
var AllTiers = []string{TierSSO, TierGoogle, TierMicrosoft, TierMagicLink, TierPasswordMFA}

// MicrosoftConsumerTenantID is the Entra tenant of personal Microsoft accounts.
const MicrosoftConsumerTenantID = "9188040d-6c67-4c5b-b112-36a304b66dad"

var tierMaxAge = map[string]time.Duration{
	TierSSO:         MaxAge,
	TierGoogle:      24 * time.Hour,
	TierMicrosoft:   24 * time.Hour,
	TierMagicLink:   4 * time.Hour,
	TierPasswordMFA: 4 * time.Hour,
}

// TierMaxAge is how long after sign-in a grant of that tier may be vouched for.
func TierMaxAge(tier string) time.Duration { return tierMaxAge[tier] }

// ValidTier reports whether tier names a known sign-in tier.
func ValidTier(tier string) bool { return slices.Contains(AllTiers, tier) }

// classify maps a recorded sign-in to its tier, or a reason it has none.
// Evidence that can't be confirmed fails closed.
func classify(row repo.GetSignInEligibilityRow) (string, Reason) {
	switch row.AuthenticationMethod.String {
	case MethodSSO:
		return TierSSO, ReasonEligible
	case MethodGoogleOAuth:
		if !onVerifiedDomain(row.IdpHostedDomain.String, row.VerifiedDomains) {
			return "", ReasonUnmanagedAccount
		}
		return TierGoogle, ReasonEligible
	case MethodMicrosoftOAuth:
		tenant := row.IdpTenantID.String
		if tenant == "" || tenant == MicrosoftConsumerTenantID || !onVerifiedDomain(row.IdpHostedDomain.String, row.VerifiedDomains) {
			return "", ReasonUnmanagedAccount
		}
		return TierMicrosoft, ReasonEligible
	case MethodMagicAuth:
		return TierMagicLink, ReasonEligible
	case MethodPassword:
		if !row.MfaVerified.Valid || !row.MfaVerified.Bool {
			return "", ReasonMFARequired
		}
		return TierPasswordMFA, ReasonEligible
	default:
		return "", ReasonMethodNotEligible
	}
}

func tierAllowed(tier string, row repo.GetSignInEligibilityRow) bool {
	if row.AllSignInMethodsAllowed {
		return true
	}
	return slices.Contains(row.AllowedSignInMethods, tier)
}

func onVerifiedDomain(domain string, verified []string) bool {
	if domain == "" {
		return false
	}
	domain = workos.NormalizeDomain(domain)
	return slices.ContainsFunc(verified, func(d string) bool { return workos.NormalizeDomain(d) == domain })
}
