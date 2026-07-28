package risk_analysis_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func nullableText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func excl(matchType, matchValue, ruleFilter, sourceFilter string) repo.RiskExclusion {
	return repo.RiskExclusion{
		MatchType:    matchType,
		MatchValue:   matchValue,
		RuleIDFilter: nullableText(ruleFilter),
		SourceFilter: nullableText(sourceFilter),
	}
}

func TestExclusionSet_Excluded(t *testing.T) {
	t.Parallel()

	finding := func(rule, source, match string) scanners.Finding {
		return scanners.Finding{RuleID: rule, Source: source, Match: match}
	}

	tests := []struct {
		name      string
		exclusion repo.RiskExclusion
		finding   scanners.Finding
		want      bool
	}{
		{"exact match suppresses", excl("exact", "test@example.com", "", ""), finding("pii.email_address", "presidio", "test@example.com"), true},
		{"exact non-match keeps", excl("exact", "test@example.com", "", ""), finding("pii.email_address", "presidio", "real@user.com"), false},
		{"regex match suppresses", excl("regex", `.*@internal\.corp$`, "", ""), finding("pii.email_address", "presidio", "bob@internal.corp"), true},
		{"regex non-match keeps", excl("regex", `.*@internal\.corp$`, "", ""), finding("pii.email_address", "presidio", "bob@gmail.com"), false},
		{"rule_id match suppresses", excl("rule_id", "pii.us_ssn", "", ""), finding("pii.us_ssn", "presidio", "123-45-6789"), true},
		{"source match suppresses", excl("source", "gitleaks", "", ""), finding("secret.aws", "gitleaks", "AKIA..."), true},
		{"entity_type match suppresses", excl("entity_type", "CREDIT_CARD", "", ""), finding("pii.credit_card", "presidio", "4111111111111111"), true},
		{"rule_id_filter narrows: match", excl("exact", "x", "pii.email_address", ""), finding("pii.email_address", "presidio", "x"), true},
		{"rule_id_filter narrows: skip", excl("exact", "x", "pii.email_address", ""), finding("pii.us_ssn", "presidio", "x"), false},
		{"source_filter narrows: skip", excl("exact", "x", "", "gitleaks"), finding("pii.us_ssn", "presidio", "x"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			set := ra.NewExclusionSet([]repo.RiskExclusion{tt.exclusion})
			require.Equal(t, tt.want, set.Excluded(tt.finding))
		})
	}
}

func TestExclusionSet_FilterFindings(t *testing.T) {
	t.Parallel()

	set := ra.NewExclusionSet([]repo.RiskExclusion{
		excl("exact", "test@example.com", "", ""),
		excl("rule_id", "pii.us_ssn", "", ""),
	})
	in := []scanners.Finding{
		{RuleID: "pii.email_address", Source: "presidio", Match: "test@example.com"}, // dropped (exact)
		{RuleID: "pii.email_address", Source: "presidio", Match: "real@user.com"},    // kept
		{RuleID: "pii.us_ssn", Source: "presidio", Match: "123-45-6789"},             // dropped (rule_id)
	}
	out := set.FilterFindings(in)
	require.Len(t, out, 1)
	require.Equal(t, "real@user.com", out[0].Match)
}

func TestExclusionSet_EmptyIsNoOp(t *testing.T) {
	t.Parallel()

	set := ra.NewExclusionSet(nil)
	require.True(t, set.Empty())
	in := []scanners.Finding{{RuleID: "pii.us_ssn", Match: "x"}}
	require.Equal(t, in, set.FilterFindings(in))
}

func TestExclusionSet_InvalidRegexSkipped(t *testing.T) {
	t.Parallel()

	// A stored regex that fails to compile is dropped defensively rather than
	// poisoning the whole set.
	set := ra.NewExclusionSet([]repo.RiskExclusion{excl("regex", "(", "", "")})
	require.True(t, set.Empty())
}
