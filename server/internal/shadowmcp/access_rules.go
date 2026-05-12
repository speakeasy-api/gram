package shadowmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	AccessRuleOutcomeAllowed      = "allowed"
	AccessRuleOutcomeDenied       = "denied"
	AccessRuleOutcomeMissingGrant = "missing_grant"
	AccessRuleOutcomeNoMatch      = "no_match"
	AccessRuleOutcomeError        = "error"

	accessRuleDispositionAllowed = "allowed"
	accessRuleDispositionDenied  = "denied"
)

type AccessRuleDecision struct {
	Outcome string
	RuleID  string
	Reason  string
}

type AccessRuleAuthorizer interface {
	RequireShadowMCPConnect(ctx context.Context, organizationID string, userID string, ruleID string, projectID string) error
}

func (d AccessRuleDecision) Allows() bool {
	return d.Outcome == AccessRuleOutcomeAllowed
}

func (c *Client) EvaluateAccessRules(ctx context.Context, authorizer AccessRuleAuthorizer, organizationID string, projectID string, userID string, evidence AccessEvidence) AccessRuleDecision {
	normalized := NormalizeAccessEvidence(evidence)
	if normalized.Empty() {
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeNoMatch,
			RuleID:  "",
			Reason:  "no Shadow MCP server identity was available for Access Rule matching",
		}
	}

	rules, err := repo.New(c.db).ListMatchingShadowMCPAccessRules(ctx, repo.ListMatchingShadowMCPAccessRulesParams{
		OrganizationID:   organizationID,
		FullUrls:         matchValues(normalized.FullURL),
		UrlHosts:         matchValues(normalized.URLHost),
		ServerIdentities: matchValues(normalized.ServerIdentity),
	})
	if err != nil {
		c.logger.WarnContext(ctx, "failed to list matching shadow mcp access rules",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
		)
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeError,
			RuleID:  "",
			Reason:  "failed to evaluate Shadow MCP Access Rules",
		}
	}

	for _, rule := range rules {
		if rule.Disposition == accessRuleDispositionDenied {
			return AccessRuleDecision{
				Outcome: AccessRuleOutcomeDenied,
				RuleID:  rule.ID.String(),
				Reason:  fmt.Sprintf("matched denied Shadow MCP Access Rule %q", rule.DisplayName),
			}
		}
	}

	matchedAllow := false
	for _, rule := range rules {
		if rule.Disposition != accessRuleDispositionAllowed {
			continue
		}
		matchedAllow = true
		if authorizer == nil {
			continue
		}
		err := authorizer.RequireShadowMCPConnect(ctx, organizationID, userID, rule.ID.String(), projectID)
		if err == nil {
			return AccessRuleDecision{
				Outcome: AccessRuleOutcomeAllowed,
				RuleID:  rule.ID.String(),
				Reason:  fmt.Sprintf("matched allowed Shadow MCP Access Rule %q", rule.DisplayName),
			}
		}
		if !isAuthzDeny(err) {
			c.logger.WarnContext(ctx, "failed shadow mcp access rule authz check",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
				attr.SlogProjectID(projectID),
				attr.SlogValueAny(map[string]any{
					"shadow_mcp_access_rule_id": rule.ID.String(),
				}),
			)
		}
	}

	if matchedAllow {
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeMissingGrant,
			RuleID:  "",
			Reason:  "matched allowed Shadow MCP Access Rule but caller is missing shadow_mcp:connect",
		}
	}

	return AccessRuleDecision{
		Outcome: AccessRuleOutcomeNoMatch,
		RuleID:  "",
		Reason:  "no matching Shadow MCP Access Rule was found",
	}
}

func isAuthzDeny(err error) bool {
	var oopsErr *oops.ShareableError
	return errors.As(err, &oopsErr) && oopsErr.Code == oops.CodeForbidden
}

func matchValues(value string) []string {
	if value == "" {
		return []string{}
	}
	return []string{value}
}
