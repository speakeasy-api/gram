package risk

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// Default dispositions for shadow MCP blocking policies, aliased from the
// shadowmcp package (which enforcement code uses directly). The disposition
// is immutable after create.
const (
	ShadowMCPDispositionBlockAll = shadowmcp.DispositionBlockAll
	ShadowMCPDispositionAllowAll = shadowmcp.DispositionAllowAll
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

// requireSingleShadowMCPBlockingPolicy rejects creating or enabling a second
// enabled blocking shadow MCP policy in a project, so block_all and allow_all
// postures can never conflict at enforcement time. excludePolicyID skips the
// policy being updated; pass uuid.Nil on create.
func requireSingleShadowMCPBlockingPolicy(ctx context.Context, queries *repo.Queries, projectID uuid.UUID, excludePolicyID uuid.UUID) error {
	policies, err := queries.ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list shadow mcp blocking policies")
	}
	for _, p := range policies {
		if p.ID == excludePolicyID || p.Action != "block" {
			continue
		}
		return oops.E(oops.CodeConflict, nil, "project already has an enabled shadow mcp blocking policy %q; disable or delete it first", p.Name)
	}
	return nil
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

// loadShadowMCPBlockedURLs reads the canonical blocked-URL set of an
// allow_all policy from its risk_policy:block grants. Block rules are
// project-wide, so only the URL selector matters — the grant audience is
// always the all-users principal.
func loadShadowMCPBlockedURLs(ctx context.Context, db repo.DBTX, organizationID string, policyID string) ([]string, error) {
	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyBlock,
		ResourceID:     policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("list shadow mcp policy block grants: %w", err)
	}
	urls := make([]string, 0, len(grants))
	for _, grant := range grants {
		if serverURL := grant.Selector[authz.SelectorKeyServerURL]; serverURL != "" {
			urls = append(urls, serverURL)
		}
	}
	slices.Sort(urls)
	return slices.Compact(urls), nil
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
