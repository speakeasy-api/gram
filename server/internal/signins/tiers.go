package signins

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/signins/repo"
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
	TierSSO       = "sso"
	TierGoogle    = "google"
	TierMicrosoft = "microsoft"
	TierMagicLink = "magic_link"
	TierPassword  = "password"
)

// AllTiers is every tier, the default when an organization sets none.
var AllTiers = []string{TierSSO, TierGoogle, TierMicrosoft, TierMagicLink, TierPassword}

// MicrosoftConsumerTenantID is the Entra tenant of personal Microsoft accounts.
const MicrosoftConsumerTenantID = "9188040d-6c67-4c5b-b112-36a304b66dad"

var tierMaxAge = map[string]time.Duration{
	TierSSO:       MaxAge,
	TierGoogle:    24 * time.Hour,
	TierMicrosoft: 24 * time.Hour,
	TierMagicLink: 4 * time.Hour,
	TierPassword:  4 * time.Hour,
}

// TierMaxAge is how long after sign-in a grant of that tier may be vouched for.
func TierMaxAge(tier string) time.Duration { return tierMaxAge[tier] }

// ValidTier reports whether tier names a known sign-in tier.
func ValidTier(tier string) bool { return slices.Contains(AllTiers, tier) }

// methodTier maps an authentication method to its tier; false when no tier accepts it.
func methodTier(method string) (string, bool) {
	switch method {
	case MethodSSO:
		return TierSSO, true
	case MethodGoogleOAuth:
		return TierGoogle, true
	case MethodMicrosoftOAuth:
		return TierMicrosoft, true
	case MethodMagicAuth:
		return TierMagicLink, true
	case MethodPassword:
		return TierPassword, true
	default:
		return "", false
	}
}

// accountEvidence checks that a social sign-in's account belongs to the
// organization. Evidence that can't be confirmed fails closed.
func accountEvidence(tier string, row repo.GetSignInEligibilityRow) Reason {
	switch tier {
	case TierGoogle:
		// '' means Google answered with no Workspace domain: a personal account.
		return managedDomain(row.IdpHostedDomain, row.VerifiedDomains, true)
	case TierMicrosoft:
		tenant := row.IdpTenantID.String
		switch {
		case strings.EqualFold(tenant, MicrosoftConsumerTenantID):
			return ReasonUnmanagedAccount
		case tenant == "":
			return ReasonAccountEvidenceUnavailable
		}
		if reason := managedDomain(row.IdpHostedDomain, row.VerifiedDomains, false); reason != ReasonEligible {
			return reason
		}
		if !tenantAllowed(tenant, row.AllowedMicrosoftTenantIds) {
			return ReasonTenantNotAllowed
		}
		return ReasonEligible
	default:
		return ReasonEligible
	}
}

// tierAllowed ignores stored values that name no known tier.
func tierAllowed(tier string, row repo.GetSignInEligibilityRow) bool {
	if row.AllSignInMethodsAllowed {
		return true
	}
	return ValidTier(tier) && slices.Contains(row.AllowedSignInMethods, tier)
}

// tenantAllowed requires the organization to have pinned the Entra tenant;
// stored values that are not tenant ids never match.
func tenantAllowed(tenant string, allowed []string) bool {
	id, err := uuid.Parse(tenant)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(allowed, func(a string) bool {
		// Only the canonical hyphenated form counts as a pin.
		pinned, err := uuid.Parse(a)
		return err == nil && strings.EqualFold(a, pinned.String()) && pinned == id && !strings.EqualFold(a, MicrosoftConsumerTenantID)
	})
}

// managedDomain: NULL is unknown evidence; an empty domain is a personal
// account only where the provider answered with none; an unverified one is the admin's.
func managedDomain(domain pgtype.Text, verified []string, emptyIsPersonal bool) Reason {
	switch {
	case !domain.Valid || (domain.String == "" && !emptyIsPersonal):
		return ReasonAccountEvidenceUnavailable
	case domain.String == "":
		return ReasonUnmanagedAccount
	case !onVerifiedDomain(domain.String, verified):
		return ReasonAccountDomainNotVerified
	default:
		return ReasonEligible
	}
}

func onVerifiedDomain(domain string, verified []string) bool {
	if domain == "" {
		return false
	}
	domain = workos.NormalizeDomain(domain)
	return slices.ContainsFunc(verified, func(d string) bool { return workos.NormalizeDomain(d) == domain })
}
