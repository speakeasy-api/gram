package ssosignins

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

// Enforced reports whether SSO sign-in linking and revocation enforcement
// are on.
func Enforced(ctx context.Context, flags feature.Provider) bool {
	return feature.SSOSignInEnforced(ctx, flags)
}

// EnableForTests turns enforcement on in an in-memory provider.
func EnableForTests(flags *feature.InMemory) {
	flags.SetFlag(feature.FlagSSOSignInEnforcement, feature.SSOSignInEnforcementDistinctID, true)
}
