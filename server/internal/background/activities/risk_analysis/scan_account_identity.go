package risk_analysis

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/accountidentity"
)

// sessionFinding attaches session-scoped findings to the batch message that
// carries them.
type sessionFinding struct {
	messageID uuid.UUID
	findings  []scanners.Finding
}

// scanAccountIdentity evaluates the batch's chats against the account
// identity rules. Unlike the content scanners it is driven by args.MessageIDs
// directly — not the scope-filtered message view — because the detector reads
// the session's account attribution, not message content: a policy scoped to
// (say) tool requests must still evaluate sessions whose batch carries only
// user messages.
//
// Findings are session-scoped rather than message-scoped: each offending chat
// gets exactly one finding per rule per policy version, attached to the
// chat's earliest in-batch message. The query reduces to one identity row per
// chat and reports which rules are already flagged at this policy version;
// only the remaining rules emit, so re-analysis and later batches of the same
// session don't accumulate duplicates, while a rule that only became
// evaluable later (identity fields arrive incrementally — e.g. the account
// email can land after classification) still fires. Chats spanning two
// concurrently analyzed batches can rarely double-emit; that is accepted as
// cosmetic.
func (a *AnalyzeBatch) scanAccountIdentity(ctx context.Context, args AnalyzeBatchArgs) ([]sessionFinding, error) {
	r := repo.New(a.db)
	rows, err := r.GetBatchChatIdentities(ctx, repo.GetBatchChatIdentitiesParams{
		Ids:               args.MessageIDs,
		ProjectID:         args.ProjectID,
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: args.PolicyVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("get batch chat identities: %w", err)
	}

	scanner := accountidentity.NewScanner(args.ApprovedEmailDomains)
	var out []sessionFinding
	for _, row := range rows {
		accountType := conv.FromPGTextOrEmpty[string](row.AccountType)
		email := conv.FromPGTextOrEmpty[string](row.Email)

		// Enrich a personal_account finding recorded on an earlier batch before
		// this account's email was known. Session findings dedupe per rule, so
		// the email-bearing finding is dropped below rather than inserted;
		// without this the original row keeps its empty match and generic
		// description forever. The update is scoped to empty-match rows so it is
		// idempotent. Only personal_account can be recorded match-less — the
		// unapproved_domain rule only fires once an email is present.
		if accountType == "personal" && email != "" &&
			slices.Contains(row.FlaggedRuleIds, scanners.GuardRuleID(accountidentity.RulePersonalAccount)) {
			ruleID, description := accountidentity.DescribePersonalAccount(email)
			if _, err := r.RefreshAccountIdentityFindingMatch(ctx, repo.RefreshAccountIdentityFindingMatchParams{
				Description:       conv.ToPGText(description),
				Match:             conv.ToPGText(email),
				ChatID:            row.ChatID,
				ProjectID:         args.ProjectID,
				RiskPolicyID:      args.RiskPolicyID,
				RiskPolicyVersion: args.PolicyVersion,
				RuleID:            conv.ToPGText(ruleID),
			}); err != nil {
				return nil, fmt.Errorf("refresh account identity finding match: %w", err)
			}
		}

		findings := scanner.Scan(accountType, email)
		findings = slices.DeleteFunc(findings, func(f scanners.Finding) bool {
			return slices.Contains(row.FlaggedRuleIds, f.RuleID)
		})
		if len(findings) == 0 {
			continue
		}
		out = append(out, sessionFinding{messageID: row.EarliestMessageID, findings: findings})
	}
	return out, nil
}
