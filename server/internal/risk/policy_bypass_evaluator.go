package risk

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type PolicyBypassEvaluation struct {
	OrganizationID string
	UserID         string
	PolicyID       string
	Target         *PolicyBypassTarget
}

// PolicyBypassTarget identifies the generic resource a bypass request or
// runtime bypass check applies to.
type PolicyBypassTarget struct {
	Kind       string
	Label      string
	Key        string
	Dimensions map[string]string
}

// IsWholePolicy reports whether the target represents a bypass for the entire
// risk policy rather than a narrower target such as a specific Shadow MCP
// server. Runtime checks use this to build a dimensionless authz check.
func (t PolicyBypassTarget) IsWholePolicy() bool {
	return t.Kind == "" && t.Key == PolicyBypassWholePolicyTargetKey && len(t.Dimensions) == 0
}

// WholePolicyBypassTarget applies to the policy as a whole.
func WholePolicyBypassTarget() PolicyBypassTarget {
	return PolicyBypassTarget{
		Kind:       "",
		Label:      "",
		Key:        PolicyBypassWholePolicyTargetKey,
		Dimensions: map[string]string{},
	}
}

// ShadowMCPServerPolicyBypassTarget applies to a specific Shadow MCP server.
func ShadowMCPServerPolicyBypassTarget(serverURL string, serverIdentity string, label string) PolicyBypassTarget {
	dimensions := map[string]string{}
	if serverURL != "" {
		dimensions[authz.SelectorKeyServerURL] = serverURL
	}
	if serverIdentity != "" {
		dimensions[authz.SelectorKeyServerIdentity] = serverIdentity
	}
	return PolicyBypassTarget{
		Kind:       PolicyBypassTargetKindShadowMCPServer,
		Label:      label,
		Key:        cmp.Or(serverURL, serverIdentity),
		Dimensions: dimensions,
	}
}

type PolicyBypassEvaluator struct {
	logger *slog.Logger
	db     repo.DBTX
}

func NewPolicyBypassEvaluator(logger *slog.Logger, db repo.DBTX) *PolicyBypassEvaluator {
	return &PolicyBypassEvaluator{
		logger: logger.With(attr.SlogComponent("risk")),
		db:     db,
	}
}

func (e *PolicyBypassEvaluator) CanBypass(ctx context.Context, input PolicyBypassEvaluation) bool {
	return e.CanBypassBatch(ctx, []PolicyBypassEvaluation{input})[input]
}

// CanBypassWithGrants evaluates a bypass against an already-loaded caller grant
// snapshot. Callers that also need policy-audience checks use this to keep both
// decisions consistent under the same transaction snapshot.
func CanBypassWithGrants(grants []authz.Grant, input PolicyBypassEvaluation) bool {
	return CanBypassBatchWithGrants(grants, []PolicyBypassEvaluation{input})[input]
}

// CanBypassBatchWithGrants evaluates several bypasses against one already-loaded
// caller grant snapshot, canonicalizing legacy selectors only once.
func CanBypassBatchWithGrants(grants []authz.Grant, inputs []PolicyBypassEvaluation) map[PolicyBypassEvaluation]bool {
	decisions := make(map[PolicyBypassEvaluation]bool, len(inputs))
	grants = canonicalizeRiskPolicyBypassGrants(grants)
	for _, input := range inputs {
		userID := strings.TrimSpace(input.UserID)
		if userID == urn.AllUsersPrincipalID || strings.TrimSpace(input.PolicyID) == "" {
			decisions[input] = false
			continue
		}
		check := authz.RiskPolicyBypassCheck(input.PolicyID, policyBypassCheckDimensions(input.Target))
		decisions[input] = authz.GrantsSatisfy(grants, check)
	}
	return decisions
}

// CanBypassBatch evaluates inputs after loading principals and grants once per
// organization/user pair. Results are keyed by their complete input so callers
// do not have to correlate parallel slices. Missing entries deny by default.
func (e *PolicyBypassEvaluator) CanBypassBatch(ctx context.Context, inputs []PolicyBypassEvaluation) map[PolicyBypassEvaluation]bool {
	results := make(map[PolicyBypassEvaluation]bool, len(inputs))
	grouped := make(map[policyBypassPrincipalKey][]PolicyBypassEvaluation)
	for _, input := range inputs {
		results[input] = false
		userID := strings.TrimSpace(input.UserID)
		if userID == urn.AllUsersPrincipalID || strings.TrimSpace(input.PolicyID) == "" {
			continue
		}
		key := policyBypassPrincipalKey{
			organizationID: input.OrganizationID,
			userID:         userID,
		}
		grouped[key] = append(grouped[key], input)
	}

	for key, evaluations := range grouped {
		grants, ok := e.loadGrants(ctx, key.organizationID, key.userID, evaluations[0].PolicyID)
		if !ok {
			continue
		}
		maps.Copy(results, CanBypassBatchWithGrants(grants, evaluations))
	}

	return results
}

type policyBypassPrincipalKey struct {
	organizationID string
	userID         string
}

func (e *PolicyBypassEvaluator) loadGrants(ctx context.Context, organizationID string, userID string, policyID string) ([]authz.Grant, bool) {
	principals, err := authz.ResolveUserPrincipals(ctx, e.db, organizationID, userID)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to resolve principals for risk policy bypass",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogUserID(userID),
			attr.SlogRiskPolicyID(policyID),
		)
		return nil, false
	}

	grants, err := authz.LoadGrants(ctx, e.db, organizationID, principals)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to load risk policy bypass grants",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogUserID(userID),
			attr.SlogRiskPolicyID(policyID),
		)
		return nil, false
	}
	return grants, true
}

// canonicalizeRiskPolicyBypassGrants keeps existing approvals compatible with
// URL-first targeting. Older access-request approvals stored both server_url
// and server_identity; the identity was metadata rather than an independent
// constraint whenever a canonical URL existed. Dropping it only for runtime
// evaluation gives those rows the same semantics as new URL-only approvals
// without changing their persisted selector (which is still needed to revoke
// or edit the original grant).
func canonicalizeRiskPolicyBypassGrants(grants []authz.Grant) []authz.Grant {
	normalized := slices.Clone(grants)
	for i := range normalized {
		selector := normalized[i].Selector
		if normalized[i].Scope != authz.ScopeRiskPolicyBypass || selector[authz.SelectorKeyServerURL] == "" {
			continue
		}
		normalized[i].Selector = maps.Clone(selector)
		if inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(selector[authz.SelectorKeyServerURL]); ok {
			normalized[i].Selector[authz.SelectorKeyServerURL] = inventoryURL.CanonicalURL
		}
		// Legacy access-request approvals paired an identity label with the URL.
		// A resolved canonical URL is the authorization target, not a second
		// constraint. Malformed legacy URLs remain URL-only and non-matching.
		delete(normalized[i].Selector, authz.SelectorKeyServerIdentity)
	}
	return normalized
}

func policyBypassCheckDimensions(target *PolicyBypassTarget) authz.RiskPolicyDimensions {
	if target == nil {
		return authz.RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}
	}
	return authz.RiskPolicyDimensions{
		ServerURL:      target.Dimensions[authz.SelectorKeyServerURL],
		ServerIdentity: target.Dimensions[authz.SelectorKeyServerIdentity],
	}
}

func ShadowMCPPolicyBypassTarget(evidence shadowmcp.AccessEvidence, toolName string) *PolicyBypassTarget {
	normalized := shadowmcp.NormalizeAccessEvidence(evidence)
	if serverURL := normalizedShadowMCPServerURL(normalized.FullURL); serverURL != "" {
		label := serverURL
		if observed := shadowmcp.ObservedName(normalized, toolName); observed != nil && *observed != "" {
			label = *observed
		}
		// A resolved URL is the canonical server key. ServerIdentity remains
		// useful display metadata, but must not become a second authorization
		// constraint: senders can report aliases that differ for the same URL.
		target := ShadowMCPServerPolicyBypassTarget(serverURL, "", label)
		return &target
	}
	if normalized.ServerIdentity != "" {
		label := normalized.ServerIdentity
		if observed := shadowmcp.ObservedName(normalized, toolName); observed != nil && *observed != "" {
			label = *observed
		}
		target := ShadowMCPServerPolicyBypassTarget("", normalized.ServerIdentity, label)
		return &target
	}
	if normalized.URLHost != "" {
		target := WholePolicyBypassTarget()
		return &target
	}
	return nil
}

func normalizedShadowMCPServerURL(raw string) string {
	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(raw)
	if !ok {
		return ""
	}
	return inventoryURL.CanonicalURL
}
