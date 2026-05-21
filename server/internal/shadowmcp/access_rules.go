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
	accessRuleResourceType       = "shadow_mcp"
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

	matchKinds, matchValues := accessRuleMatchCandidates(normalized)
	rules, err := repo.New(c.db).ListMatchingAccessRules(ctx, repo.ListMatchingAccessRulesParams{
		OrganizationID: organizationID,
		ResourceType:   accessRuleResourceType,
		ProjectID:      uuid.NullUUID{UUID: parsedProjectID, Valid: true},
		MatchKinds:     matchKinds,
		MatchValues:    matchValues,
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

func accessRuleMatchCandidates(evidence AccessEvidence) ([]string, []string) {
	kinds := make([]string, 0, 3)
	values := make([]string, 0, 3)
	if evidence.FullURL != "" {
		kinds = append(kinds, MatchBreadthFullURL)
		values = append(values, evidence.FullURL)
	}
	if evidence.URLHost != "" {
		kinds = append(kinds, MatchBreadthURLHost)
		values = append(values, evidence.URLHost)
	}
	if evidence.ServerIdentity != "" {
		kinds = append(kinds, MatchBreadthServerIdentity)
		values = append(values, evidence.ServerIdentity)
	}
	return kinds, values
}
