package risk

import (
	"context"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ShadowMCPPolicyURLReconciler replaces the URL grants owned by one risk policy.
type ShadowMCPPolicyURLReconciler func(
	ctx context.Context,
	db repo.DBTX,
	input policybypass.ReconcilePolicyURLsInput,
) error

func validateShadowMCPAllowedURLs(enabled bool, sources []string, action string, rawURLs []string) ([]string, error) {
	canonicalURLs, err := policybypass.CanonicalizeURLs(rawURLs)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid shadow mcp allowed urls")
	}
	if len(canonicalURLs) > 0 && (!enabled || action != "block" || !slices.Contains(sources, "shadow_mcp")) {
		return nil, oops.E(oops.CodeInvalid, nil, "shadow mcp allowed urls require an enabled blocking shadow mcp policy")
	}
	return canonicalURLs, nil
}
