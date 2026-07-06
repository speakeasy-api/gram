package risk_analysis

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// SourceAccountIdentity is the policy source value flagging sessions
// authenticated with a non-corporate AI account. Unlike the content scanners
// it inspects the chat's account attribution (personal-account tracking data
// on user_accounts), not the message text.
const SourceAccountIdentity = "account_identity"

// DescribeIdentityPersonalAccount returns the canonical rule and description.
func DescribeIdentityPersonalAccount(email string) (string, string) {
	if email == "" {
		return guard(RuleIdentityPersonalAccount), "Session authenticated with a personal AI account."
	}
	return guard(RuleIdentityPersonalAccount), fmt.Sprintf("Session authenticated with the personal AI account %q.", email)
}

// DescribeIdentityUnapprovedDomain returns the canonical rule and description.
func DescribeIdentityUnapprovedDomain(email string) (string, string) {
	return guard(RuleIdentityUnapprovedDomain), fmt.Sprintf("Session authenticated with the AI account %q, whose email domain is not on the approved corporate domain list.", email)
}

// sessionFinding attaches session-scoped findings to the batch message that
// carries them.
type sessionFinding struct {
	messageID uuid.UUID
	findings  []Finding
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
	rows, err := repo.New(a.db).GetBatchChatIdentities(ctx, repo.GetBatchChatIdentitiesParams{
		Ids:               args.MessageIDs,
		ProjectID:         args.ProjectID,
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: args.PolicyVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("get batch chat identities: %w", err)
	}

	approvedDomains := normalizeApprovedDomains(args.ApprovedEmailDomains)
	var out []sessionFinding
	for _, row := range rows {
		findings := evaluateAccountIdentity(
			conv.FromPGTextOrEmpty[string](row.AccountType),
			conv.FromPGTextOrEmpty[string](row.Email),
			approvedDomains,
		)
		findings = slices.DeleteFunc(findings, func(f Finding) bool {
			return slices.Contains(row.FlaggedRuleIds, f.RuleID)
		})
		if len(findings) == 0 {
			continue
		}
		out = append(out, sessionFinding{messageID: row.EarliestMessageID, findings: findings})
	}
	return out, nil
}

func evaluateAccountIdentity(accountType string, email string, approvedDomains map[string]struct{}) []Finding {
	var findings []Finding
	if accountType == "personal" {
		ruleID, description := DescribeIdentityPersonalAccount(email)
		findings = append(findings, accountIdentityFinding(ruleID, description, email))
	}
	if len(approvedDomains) > 0 && email != "" {
		if domain := emailDomain(email); domain != "" {
			if _, ok := approvedDomains[domain]; !ok {
				ruleID, description := DescribeIdentityUnapprovedDomain(email)
				findings = append(findings, accountIdentityFinding(ruleID, description, email))
			}
		}
	}
	return findings
}

func accountIdentityFinding(ruleID string, description string, email string) Finding {
	return Finding{
		Source:              SourceAccountIdentity,
		RuleID:              ruleID,
		Description:         description,
		Match:               email,
		StartPos:            0,
		EndPos:              0,
		Tags:                []string{},
		Confidence:          1.0,
		DeadLetterReason:    "",
		mcpLookupToolCallID: "",
		spanGroupKey:        "",
		field:               "",
		path:                "",
	}
}

// normalizeApprovedDomains lowercases entries and strips a leading "@" so
// "@Acme.com" and "acme.com" configure the same domain. Matching is exact:
// subdomains must be listed explicitly.
func normalizeApprovedDomains(domains []string) map[string]struct{} {
	if len(domains) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		domain = strings.TrimPrefix(domain, "@")
		if domain == "" {
			continue
		}
		out[domain] = struct{}{}
	}
	return out
}

// emailDomain extracts the lowercased domain of an email address, or "" when
// the value has no usable domain part.
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}
