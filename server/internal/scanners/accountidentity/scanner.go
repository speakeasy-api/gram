// Package accountidentity is the single home for the account-identity scanner.
// Unlike the content scanners it inspects a chat session's account attribution
// (personal-account tracking data on user_accounts), not the message text: it
// flags sessions authenticated with a non-corporate AI account and converts
// each match into the shared scanners.Finding domain type.
//
// The scanner is pure and stateless: given a session's account type and email
// plus the policy's approved corporate domain list, Scan returns the
// session-scoped findings. The batch activity owns the surrounding DB reads,
// per-rule dedupe, and in-place match enrichment; this package owns only the
// detection rules and their canonical descriptions.
package accountidentity

import (
	"context"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// Source labels findings produced by this scanner. Unlike the content scanners
// it inspects the chat's account attribution, not message content.
const Source = "account_identity"

const prefix = "identity."

const (
	// RulePersonalAccount fires when a session's AI account is classified as a
	// personal (non-team) account.
	RulePersonalAccount = prefix + "personal_account"

	// RuleUnapprovedDomain fires when a session's AI-account email domain is
	// not on the policy's approved corporate domain list.
	RuleUnapprovedDomain = prefix + "unapproved_domain"
)

// DescribePersonalAccount returns the canonical (rule_id, description) for a
// personal-account finding. The email is embedded when known so a finding
// recorded before the account's email was learned can be enriched later.
func DescribePersonalAccount(email string) (string, string) {
	if email == "" {
		return scanners.GuardRuleID(RulePersonalAccount), "Session authenticated with a personal AI account."
	}
	return scanners.GuardRuleID(RulePersonalAccount), fmt.Sprintf("Session authenticated with the personal AI account %q.", email)
}

// DescribeUnapprovedDomain returns the canonical (rule_id, description) for an
// unapproved-domain finding.
func DescribeUnapprovedDomain(email string) (string, string) {
	return scanners.GuardRuleID(RuleUnapprovedDomain), fmt.Sprintf("Session authenticated with the AI account %q, whose email domain is not on the approved corporate domain list.", email)
}

// ScanRequest is one chat session's account attribution to evaluate.
// AccountType and Email are the session's resolved account fields (either may
// be empty when the account is unclassified or its email is not yet known);
// ApprovedDomains is the policy's approved corporate email domain list.
type ScanRequest struct {
	ApprovedDomains []string
	AccountType     string
	Email           string
}

// Scanner evaluates a chat session's account attribution against the account
// identity rules. It is stateless and safe for concurrent use.
type Scanner struct{}

// NewScanner returns a ready-to-use Scanner.
func NewScanner() *Scanner { return &Scanner{} }

// Scan returns the session-scoped findings for one chat's account attribution.
//
// A personal account always flags. An email whose domain is not on the approved
// list flags only when a domain list is configured — matching is exact, so
// subdomains must be listed explicitly. Findings carry the email as their Match
// (empty when unknown), so an early match-less finding can be enriched later.
func (s *Scanner) Scan(_ context.Context, req ScanRequest) []scanners.Finding {
	var findings []scanners.Finding
	if req.AccountType == "personal" {
		ruleID, description := DescribePersonalAccount(req.Email)
		findings = append(findings, finding(ruleID, description, req.Email))
	}
	if approved := normalizeApprovedDomains(req.ApprovedDomains); len(approved) > 0 && req.Email != "" {
		if domain := emailDomain(req.Email); domain != "" {
			if _, ok := approved[domain]; !ok {
				ruleID, description := DescribeUnapprovedDomain(req.Email)
				findings = append(findings, finding(ruleID, description, req.Email))
			}
		}
	}
	return findings
}

// finding builds an account-identity Finding. The match is the account email
// (empty when unknown) and positions are zero: findings are session-scoped, not
// anchored to a span of message text.
func finding(ruleID string, description string, email string) scanners.Finding {
	return scanners.Finding{
		Source:      Source,
		RuleID:      ruleID,
		Description: description,
		Match:       email,
		StartPos:    0,
		EndPos:      0,
		Tags:        []string{},
		Confidence:  1.0,

		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
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
