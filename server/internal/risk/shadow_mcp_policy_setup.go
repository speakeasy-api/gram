package risk

import (
	"context"
	"slices"

	"github.com/google/uuid"

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

// ShadowMCPInventoryURLLookup returns the requested canonical URLs that were
// observed in the authenticated project inventory.
type ShadowMCPInventoryURLLookup func(
	ctx context.Context,
	projectID uuid.UUID,
	canonicalURLs []string,
) ([]string, error)

func validateShadowMCPAllowedURLs(
	ctx context.Context,
	lookup ShadowMCPInventoryURLLookup,
	projectID uuid.UUID,
	enabled bool,
	sources []string,
	action string,
	rawURLs []string,
) ([]string, error) {
	canonicalURLs, err := policybypass.CanonicalizeURLs(rawURLs)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid shadow mcp allowed urls")
	}
	if len(canonicalURLs) > 0 && (!enabled || action != "block" || !slices.Contains(sources, "shadow_mcp")) {
		return nil, oops.E(oops.CodeInvalid, nil, "shadow mcp allowed urls require an enabled blocking shadow mcp policy")
	}
	if len(canonicalURLs) == 0 {
		return canonicalURLs, nil
	}

	observedURLs, err := lookup(ctx, projectID, canonicalURLs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "validate shadow mcp allowed url inventory")
	}
	observedURLSet := make(map[string]struct{}, len(observedURLs))
	for _, observedURL := range observedURLs {
		observedURLSet[observedURL] = struct{}{}
	}
	for _, canonicalURL := range canonicalURLs {
		if _, observed := observedURLSet[canonicalURL]; !observed {
			return nil, oops.E(oops.CodeInvalid, nil, "shadow mcp allowed url %q has not been observed in this project", canonicalURL)
		}
	}
	return canonicalURLs, nil
}
