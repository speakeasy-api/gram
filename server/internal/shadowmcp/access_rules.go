package shadowmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	AccessRuleOutcomeAllowed = "allowed"
	AccessRuleOutcomeDenied  = "denied"
	AccessRuleOutcomeNoMatch = "no_match"
	AccessRuleOutcomeError   = "error"

	accessRuleDispositionAllowed = "allowed"
	accessRuleDispositionDenied  = "denied"
)

type AccessRuleDecision struct {
	Outcome string
	RuleID  string
	Reason  string
}

func (d AccessRuleDecision) Allows() bool {
	return d.Outcome == AccessRuleOutcomeAllowed
}

func (c *Client) EvaluateAccessRules(ctx context.Context, organizationID string, projectID string, evidence AccessEvidence) AccessRuleDecision {
	normalized := NormalizeAccessEvidence(evidence)
	if normalized.Empty() {
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeNoMatch,
			RuleID:  "",
			Reason:  "no Shadow MCP server identity was available for Access Rule matching",
		}
	}
	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		c.logger.WarnContext(ctx, "failed to parse shadow mcp project id",
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

	rules, err := repo.New(c.db).ListMatchingShadowMCPAccessRules(ctx, repo.ListMatchingShadowMCPAccessRulesParams{
		OrganizationID:   organizationID,
		ProjectID:        uuid.NullUUID{UUID: parsedProjectID, Valid: true},
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

	for _, rule := range rules {
		if rule.Disposition != accessRuleDispositionAllowed {
			continue
		}
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeAllowed,
			RuleID:  rule.ID.String(),
			Reason:  fmt.Sprintf("matched allowed Shadow MCP Access Rule %q", rule.DisplayName),
		}
	}

	return AccessRuleDecision{
		Outcome: AccessRuleOutcomeNoMatch,
		RuleID:  "",
		Reason:  "no matching Shadow MCP Access Rule was found",
	}
}

func matchValues(value string) []string {
	if value == "" {
		return []string{}
	}
	return []string{value}
}
