package ssosignins

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/ssosignins/repo"
)

// Enforced reports whether SSO sign-in linking and revocation enforcement
// are on for an organization. Callers evaluate it once per request.
func Enforced(ctx context.Context, flags feature.Provider, organizationID string) bool {
	return feature.SSOSignInEnforced(ctx, flags, organizationID)
}

// EnableForTests turns enforcement on for the given organizations in an
// in-memory provider.
func EnableForTests(flags *feature.InMemory, organizationID string, more ...string) {
	for _, organizationID := range append([]string{organizationID}, more...) {
		flags.SetFlag(feature.FlagSSOSignInEnforcement, organizationID, true)
	}
}

// EvidenceGate reports whether enforcement is on for the organization a
// WorkOS sign-in selected, so evidence lookups only run where they count.
func EvidenceGate(db repo.DBTX, flags feature.Provider) func(ctx context.Context, workosOrganizationID string) bool {
	return func(ctx context.Context, workosOrganizationID string) bool {
		if workosOrganizationID == "" {
			return false
		}
		organizationID, err := repo.New(db).GetOrganizationIDByWorkOSID(ctx, conv.ToPGText(workosOrganizationID))
		if err != nil {
			return false
		}
		return Enforced(ctx, flags, organizationID)
	}
}
