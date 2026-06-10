package risk

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
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
	authz  *authz.Engine
}

func NewPolicyBypassEvaluator(logger *slog.Logger, db repo.DBTX, authzEngine *authz.Engine) *PolicyBypassEvaluator {
	return &PolicyBypassEvaluator{
		logger: logger.With(attr.SlogComponent("risk")),
		db:     db,
		authz:  authzEngine,
	}
}

func (e *PolicyBypassEvaluator) CanBypass(ctx context.Context, input PolicyBypassEvaluation) bool {
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.PolicyID) == "" {
		return false
	}

	grants, ok := e.loadGrants(ctx, input)
	if !ok {
		return false
	}

	check := authz.RiskPolicyBypassCheck(input.PolicyID, policyBypassCheckDimensions(input.Target))
	return e.authz.EvaluateLoadedGrants(ctx, grants, check) == nil
}

func (e *PolicyBypassEvaluator) loadGrants(ctx context.Context, input PolicyBypassEvaluation) ([]authz.Grant, bool) {
	principals, err := authz.ResolveUserPrincipals(ctx, e.db, input.OrganizationID, input.UserID)
	if err != nil {
		if !errors.Is(err, authz.ErrPrincipalNotFound) {
			e.logger.WarnContext(ctx, "failed to resolve principals for risk policy bypass",
				attr.SlogError(err),
				attr.SlogOrganizationID(input.OrganizationID),
				attr.SlogUserID(input.UserID),
				attr.SlogRiskPolicyID(input.PolicyID),
			)
		}
		return nil, false
	}
	grants, err := authz.LoadGrants(ctx, e.db, input.OrganizationID, principals)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to load risk policy bypass grants",
			attr.SlogError(err),
			attr.SlogOrganizationID(input.OrganizationID),
			attr.SlogUserID(input.UserID),
			attr.SlogRiskPolicyID(input.PolicyID),
		)
		return nil, false
	}
	return grants, true
}

func policyBypassCheckDimensions(target *PolicyBypassTarget) authz.RiskPolicyBypassDimensions {
	if target == nil {
		return authz.RiskPolicyBypassDimensions{ServerURL: "", ServerIdentity: ""}
	}
	return authz.RiskPolicyBypassDimensions{
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
		target := ShadowMCPServerPolicyBypassTarget(serverURL, normalized.ServerIdentity, label)
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
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}
