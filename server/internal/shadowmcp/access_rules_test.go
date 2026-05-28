package shadowmcp_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestEvaluateAccessRules_AllowsMatchingOrganizationRule(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	rule := f.createAccessRule(t, "allowed", "full_url", "https://mcp.example.com/v1")

	decision := f.client.EvaluateAccessRules(t.Context(), f.orgID, f.projectID.String(), shadowmcp.AccessEvidence{
		FullURL:        "https://mcp.example.com/v1",
		URLHost:        "",
		ServerIdentity: "",
	})

	require.Equal(t, shadowmcp.AccessRuleOutcomeAllowed, decision.Outcome)
	require.Equal(t, rule.ID, decision.RuleID)
}

func TestEvaluateAccessRules_DenyRuleWins(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.createAccessRule(t, "allowed", "url_host", "mcp.example.com")
	denied := f.createAccessRule(t, "denied", "full_url", "https://mcp.example.com/v1")

	decision := f.client.EvaluateAccessRules(t.Context(), f.orgID, f.projectID.String(), shadowmcp.AccessEvidence{
		FullURL:        "https://mcp.example.com/v1",
		URLHost:        "",
		ServerIdentity: "",
	})

	require.Equal(t, shadowmcp.AccessRuleOutcomeDenied, decision.Outcome)
	require.Equal(t, denied.ID, decision.RuleID)
}

func TestEvaluateAccessRules_IgnoresServerIdentityRules(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	otherProjectID := uuid.New()
	rule := f.createProjectAccessRule(t, f.projectID, "allowed", accesscontrol.MatchKindServerIdentity, "github")

	allowed := f.client.EvaluateAccessRules(t.Context(), f.orgID, f.projectID.String(), shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: "github",
	})
	require.Equal(t, shadowmcp.AccessRuleOutcomeNoMatch, allowed.Outcome)
	require.Empty(t, allowed.RuleID)

	blocked := f.client.EvaluateAccessRules(t.Context(), f.orgID, otherProjectID.String(), shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: "github",
	})
	require.Equal(t, shadowmcp.AccessRuleOutcomeNoMatch, blocked.Outcome)
	require.Empty(t, blocked.RuleID)
	require.NotEmpty(t, rule.ID)
}

func (f *fixture) createAccessRule(t *testing.T, disposition string, matchBreadth string, matchValue string) accesscontrol.AccessRule {
	t.Helper()
	return f.createAccessRuleWithScope(t, "", accesscontrol.AccessScopeOrganization, disposition, matchBreadth, matchValue)
}

func (f *fixture) createProjectAccessRule(t *testing.T, projectID uuid.UUID, disposition string, matchBreadth string, matchValue string) accesscontrol.AccessRule {
	t.Helper()
	return f.createAccessRuleWithScope(t, projectID.String(), accesscontrol.AccessScopeProject, disposition, matchBreadth, matchValue)
}

func (f *fixture) createAccessRuleWithScope(t *testing.T, projectID string, accessScope string, disposition string, matchBreadth string, matchValue string) accesscontrol.AccessRule {
	t.Helper()
	now := time.Now().UTC()
	rule, err := f.accessStore.CreateRule(t.Context(), accesscontrol.AccessRule{
		ID:             uuid.NewString(),
		OrganizationID: f.orgID,
		ProjectID:      projectID,
		AccessScope:    accessScope,
		ResourceType:   accesscontrol.ResourceTypeShadowMCP,
		Disposition:    disposition,
		MatchKind:      matchBreadth,
		MatchValue:     matchValue,
		DisplayName:    matchValue,
		ObservedSummary: accesscontrol.ObservedSummary{
			Name:           nil,
			FullURL:        nil,
			URLHost:        nil,
			ServerIdentity: nil,
			ToolName:       nil,
			ToolCall:       nil,
			BlockReason:    nil,
			RiskPolicyID:   nil,
			RiskResultID:   nil,
		},
		SourceRequestID: "",
		CreatedBy:       "",
		UpdatedBy:       "",
		Reason:          "",
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	require.NoError(t, err)
	return rule
}
