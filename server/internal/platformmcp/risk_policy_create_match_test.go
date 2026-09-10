package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestRiskPolicyCreateMatchesRejectsLegacyNarrowedRows(t *testing.T) {
	t.Parallel()

	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	projectID := uuid.New()
	desired := riskrepo.CreateRiskPolicyParams{
		ID: uuid.New(), ProjectID: projectID, OrganizationID: "<ORG_ID>", Name: "Policy", PolicyType: "standard",
		Sources: []string{"gitleaks"}, PresidioEntities: []string{}, AnalyzerConfig: []byte(`{}`), PromptInjectionRules: []string{}, DisabledRules: []string{}, CustomRuleIds: []string{},
		MessageTypes: nil, ScopeInclude: pgtype.Text{}, ScopeExempt: pgtype.Text{}, Enabled: true, Action: "flag", AudienceType: riskPolicyAudienceEveryone,
		ShadowMcpDisposition: pgtype.Text{}, AutoName: false, UserMessage: pgtype.Text{}, Prompt: pgtype.Text{}, ModelConfig: nil, Score: pgtype.Float8{Float64: 5, Valid: true},
	}
	row := riskrepo.RiskPolicy{
		ID: uuid.New(), ProjectID: projectID, OrganizationID: "<ORG_ID>", Name: "Policy", PolicyType: "standard",
		Sources: []string{"gitleaks"}, PresidioEntities: []string{}, AnalyzerConfig: []byte(`{}`), PromptInjectionRules: []string{}, DisabledRules: []string{}, CustomRuleIds: []string{},
		Enabled: true, Action: "flag", AudienceType: riskPolicyAudienceEveryone, Score: 5,
	}
	audience := []string{authz.AllUsersPrincipal().String()}

	cases := map[string]struct {
		messageTypes []string
		matches      bool
	}{
		"null":         {messageTypes: nil, matches: true},
		"full catalog": {messageTypes: []string{"user_message", "tool_response", "assistant_message", "tool_request"}, matches: true},
		"narrowed":     {messageTypes: []string{"tool_request", "tool_response"}, matches: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidate := row
			candidate.MessageTypes = tc.messageTypes
			require.Equal(t, tc.matches, riskPolicyCreateMatches(candidate, audience, desired, catalog))
		})
	}
}
