package accountidentity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/accountidentity"
)

func rules(findings []scanners.Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.RuleID
	}
	return out
}

func TestScanner_PersonalAccountAlwaysFlags(t *testing.T) {
	t.Parallel()

	findings := accountidentity.NewScanner().Scan(t.Context(), accountidentity.ScanRequest{
		ApprovedDomains: nil,
		AccountType:     "personal",
		Email:           "jane@gmail.com",
	})
	require.Len(t, findings, 1)
	assert.Equal(t, accountidentity.RulePersonalAccount, findings[0].RuleID)
	assert.Equal(t, accountidentity.Source, findings[0].Source)
	assert.Equal(t, "jane@gmail.com", findings[0].Match)
	assert.Equal(t, `Session authenticated with the personal AI account "jane@gmail.com".`, findings[0].Description)
}

func TestScanner_PersonalAccountWithoutEmailIsGeneric(t *testing.T) {
	t.Parallel()

	findings := accountidentity.NewScanner().Scan(t.Context(), accountidentity.ScanRequest{
		ApprovedDomains: nil,
		AccountType:     "personal",
		Email:           "",
	})
	require.Len(t, findings, 1)
	assert.Empty(t, findings[0].Match)
	assert.Equal(t, "Session authenticated with a personal AI account.", findings[0].Description)
}

func TestScanner_TeamAccountWithoutDomainListInert(t *testing.T) {
	t.Parallel()

	findings := accountidentity.NewScanner().Scan(t.Context(), accountidentity.ScanRequest{
		ApprovedDomains: nil,
		AccountType:     "team",
		Email:           "bob@other.com",
	})
	require.Empty(t, findings)
}

func TestScanner_UnapprovedDomainMatrix(t *testing.T) {
	t.Parallel()

	scanner := accountidentity.NewScanner()

	cases := []struct {
		accountType string
		email       string
		wantRules   []string
	}{
		{"team", "alice@acme.com", nil},
		{"team", "DAVE@ACME.COM", nil},
		{"team", "bob@other.com", []string{accountidentity.RuleUnapprovedDomain}},
		// Exact-match semantics: subdomains must be listed explicitly.
		{"team", "carol@mail.acme.com", []string{accountidentity.RuleUnapprovedDomain}},
		{"personal", "eve@acme.com", []string{accountidentity.RulePersonalAccount}},
		{"personal", "frank@other.com", []string{accountidentity.RulePersonalAccount, accountidentity.RuleUnapprovedDomain}},
	}

	for _, tc := range cases {
		// Domain matching is exact and case-insensitive; a leading "@" is stripped.
		findings := scanner.Scan(t.Context(), accountidentity.ScanRequest{
			ApprovedDomains: []string{"@Acme.com"},
			AccountType:     tc.accountType,
			Email:           tc.email,
		})
		assert.ElementsMatch(t, tc.wantRules, rules(findings), "email %s", tc.email)
	}
}

func TestScanner_UnapprovedDomainNeedsEmailDomain(t *testing.T) {
	t.Parallel()

	scanner := accountidentity.NewScanner()

	// A malformed email with no usable domain part cannot be classified against
	// the domain list, so the unapproved_domain rule stays inert.
	require.Empty(t, scanner.Scan(t.Context(), accountidentity.ScanRequest{
		ApprovedDomains: []string{"acme.com"},
		AccountType:     "team",
		Email:           "no-at-sign",
	}))
	require.Empty(t, scanner.Scan(t.Context(), accountidentity.ScanRequest{
		ApprovedDomains: []string{"acme.com"},
		AccountType:     "team",
		Email:           "trailing@",
	}))
}
