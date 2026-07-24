package risk

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// Default dispositions for shadow MCP blocking policies. block_all denies
// every non-Gram-hosted server unless explicitly allowed (the original
// behavior); allow_all permits every server unless it appears on the policy's
// blocked-URL list. The disposition is immutable after create.
//
// Must stay in sync with RiskPolicyShadowMCPDispositionEnum in
// server/design/shared/risk.go — the design package cannot import this one.
const (
	ShadowMCPDispositionBlockAll = "block_all"
	ShadowMCPDispositionAllowAll = "allow_all"
)

// validateShadowMCPDisposition rejects a disposition on anything other than a
// blocking shadow MCP policy. An empty disposition is always valid: it means
// the caller did not choose one and block_all semantics apply.
func validateShadowMCPDisposition(disposition string, sources []string, action string) error {
	switch disposition {
	case "", ShadowMCPDispositionBlockAll, ShadowMCPDispositionAllowAll:
	default:
		return oops.E(oops.CodeInvalid, nil, "invalid shadow mcp disposition %q", disposition)
	}
	if disposition != "" && (action != "block" || !slices.Contains(sources, shadowmcp.SourceShadowMCP)) {
		return oops.E(oops.CodeInvalid, nil, "shadow mcp disposition requires a blocking shadow mcp policy")
	}
	return nil
}

// shadowMCPPolicyAutoName returns the fixed auto-generated name for a policy
// that only blocks shadow MCP servers. These policies are structural (at most
// one enabled per project), so the LLM namer is skipped for them. Returns ""
// for every other policy shape, letting the regular naming flow run.
func shadowMCPPolicyAutoName(sources []string, action string, existingNames []string) string {
	if action != "block" || len(sources) != 1 || sources[0] != shadowmcp.SourceShadowMCP {
		return ""
	}
	const base = "Shadow MCP Server Policy"
	name := base
	for suffix := 2; slices.Contains(existingNames, name); suffix++ {
		name = fmt.Sprintf("%s %d", base, suffix)
	}
	return name
}

// validateShadowMCPBlockedURLs canonicalizes the blocked-URL set of an
// allow_all policy. Unlike the allow list there is no inventory-observed
// requirement: blocking a server that has not been seen yet is deliberate,
// proactive defense. A non-empty list is only valid on an allow_all policy.
func validateShadowMCPBlockedURLs(disposition string, rawURLs []string) ([]string, error) {
	canonicalURLs, err := policybypass.CanonicalizeURLs(rawURLs)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid shadow mcp blocked urls")
	}
	if len(canonicalURLs) > 0 && disposition != ShadowMCPDispositionAllowAll {
		return nil, oops.E(oops.CodeInvalid, nil, "shadow mcp blocked urls require an allow_all shadow mcp policy")
	}
	return canonicalURLs, nil
}

// effectiveShadowMCPDisposition resolves the stored disposition column for a
// policy: block_all when unset on a blocking shadow MCP policy (rows created
// before the column existed), empty for policies where a disposition does not
// apply.
func effectiveShadowMCPDisposition(disposition pgtype.Text, sources []string, action string) string {
	if action != "block" || !slices.Contains(sources, shadowmcp.SourceShadowMCP) {
		return ""
	}
	if disposition.Valid && disposition.String != "" {
		return disposition.String
	}
	return ShadowMCPDispositionBlockAll
}

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
	disposition string,
	rawURLs []string,
) ([]string, error) {
	canonicalURLs, err := policybypass.CanonicalizeURLs(rawURLs)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid shadow mcp allowed urls")
	}
	if len(canonicalURLs) > 0 && (!enabled || action != "block" || !slices.Contains(sources, "shadow_mcp")) {
		return nil, oops.E(oops.CodeInvalid, nil, "shadow mcp allowed urls require an enabled blocking shadow mcp policy")
	}
	if len(canonicalURLs) > 0 && disposition == ShadowMCPDispositionAllowAll {
		return nil, oops.E(oops.CodeInvalid, nil, "shadow mcp allowed urls do not apply to allow_all policies; use shadow_mcp_blocked_urls")
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
