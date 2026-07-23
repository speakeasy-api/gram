package gitleaks

import (
	"regexp"

	"github.com/zricethezav/gitleaks/v8/config"
)

// Canonical rule ids (post-CanonicalRuleID) for the AWS credential findings.
// These are the shared identifiers the rest of the system keys on — categories
// classification (via the `secret.` prefix) and, crucially, the redaction
// policy, which passes the access key id through unmasked while redacting the
// secret access key and session token.
const (
	// AccessKeyIDRuleID is the gitleaks built-in "aws-access-token" rule. The
	// value it matches is an identifier (the AKIA/ASIA-prefixed access key id),
	// not a secret — AWS itself logs it in CloudTrail — so the redaction layer
	// passes it through unmasked.
	AccessKeyIDRuleID = "secret.aws_access_token"
	// SecretAccessKeyRuleID is our added "aws-secret-access-key" rule.
	SecretAccessKeyRuleID = "secret.aws_secret_access_key" //nolint:gosec // G101: a rule identifier, not a credential
	// SessionTokenRuleID is our added "aws-session-token" rule.
	SessionTokenRuleID = "secret.aws_session_token" //nolint:gosec // G101: a rule identifier, not a credential
)

// awsAccessTokenRuleID is the gitleaks (pre-canonical) rule id for the built-in
// AWS access key id rule. Our composite secret rule requires it as an anchor.
const awsAccessTokenRuleID = "aws-access-token"

func intPtr(i int) *int { return &i }

// awsRules returns the custom rules layered onto gitleaks' default config to
// close its AWS coverage gap. The stock config detects only the access key id
// (AKIA/ASIA...), which is an identifier; it ships no rule for the two actual
// secrets — the secret access key and the session token. These rules add them
// using gitleaks' native mechanisms:
//
//   - aws-secret-access-key is a native COMPOSITE rule: a bare 40-char base64
//     blob (SecretGroup 1) is only reported when an aws-access-token finding
//     sits within WithinLines of it (RequiredRules). This is gitleaks-native
//     anchored detection — it mirrors how AWS credentials actually travel (the
//     secret always accompanies its access key id, in an STS response, an INI
//     profile, or an `aws configure` dump) and needs no per-secret label. The
//     entropy floor (4.1, above the 4.0 ceiling of 40-char lowercase hex)
//     rejects SHA-1 / git object id false positives. A single secret rule keeps
//     the reported rule id deterministic (two rules matching the same span would
//     race on Go map iteration order).
//   - aws-session-token uses the contextual-regex pattern gitleaks' own
//     generic-api-key rule uses: the token's conventional label captured with
//     the value (SecretGroup 1). Session tokens are effectively always labeled
//     (AWS_SESSION_TOKEN, the STS SessionToken field, or the x-amz-security-token
//     header), so a label anchor fits them where an id anchor fits the secret.
//
// The built-in aws-access-token rule still fires independently, so the id is
// always reported alongside the secret it anchors.
func awsRules() []config.Rule {
	return []config.Rule{
		//nolint:exhaustruct // third-party gitleaks rule; only the set fields are relevant
		{
			RuleID:      "aws-secret-access-key",
			Description: "Identified a probable AWS secret access key co-located with an AWS access key id.",
			Regex:       regexp.MustCompile(`\b([A-Za-z0-9/+]{40})\b`),
			SecretGroup: 1,
			Entropy:     4.1,
			// Prefilter on the access-key-id prefixes so this broad regex only runs
			// on fragments that could contain an anchor in the first place.
			Keywords: []string{"akia", "asia", "abia", "acca", "a3t", "agpa", "aida", "aipa", "anpa", "anva", "aroa", "apka", "asca"},
			RequiredRules: []*config.Required{
				{RuleID: awsAccessTokenRuleID, WithinLines: intPtr(4)},
			},
			Tags: []string{"aws"},
		},
		//nolint:exhaustruct // third-party gitleaks rule; only the set fields are relevant
		{
			RuleID:      "aws-session-token",
			Description: "Identified a probable AWS session (STS security) token adjacent to its credential label.",
			// (aws_)session/security_token <sep> <long base64/base64url>. Session
			// tokens run to hundreds of chars; the 100-char floor keeps ordinary
			// values out.
			Regex:       regexp.MustCompile(`(?i)(?:aws[_.\-]?)?(?:session|security)[_.\-]?token["'` + "`" + `\s:=,]{1,8}([A-Za-z0-9/+_=\-]{100,})`),
			SecretGroup: 1,
			Keywords:    []string{"token"},
			Tags:        []string{"aws"},
		},
	}
}
