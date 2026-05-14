package shadowmcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
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
	require.Equal(t, rule.ID.String(), decision.RuleID)
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
	require.Equal(t, denied.ID.String(), decision.RuleID)
}

func TestEvaluateAccessRules_ProjectRuleOnlyMatchesSameProject(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	otherProjectID := uuid.New()
	rule := f.createProjectAccessRule(t, f.projectID, "allowed", "server_identity", "github")

	allowed := f.client.EvaluateAccessRules(t.Context(), f.orgID, f.projectID.String(), shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: "github",
	})
	require.Equal(t, shadowmcp.AccessRuleOutcomeAllowed, allowed.Outcome)
	require.Equal(t, rule.ID.String(), allowed.RuleID)

	blocked := f.client.EvaluateAccessRules(t.Context(), f.orgID, otherProjectID.String(), shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: "github",
	})
	require.Equal(t, shadowmcp.AccessRuleOutcomeNoMatch, blocked.Outcome)
}

func (f *fixture) createAccessRule(t *testing.T, disposition string, matchBreadth string, matchValue string) accessrepo.ShadowMcpAccessRule {
	t.Helper()
	return f.createAccessRuleWithScope(t, uuid.NullUUID{}, "organization", disposition, matchBreadth, matchValue)
}

func (f *fixture) createProjectAccessRule(t *testing.T, projectID uuid.UUID, disposition string, matchBreadth string, matchValue string) accessrepo.ShadowMcpAccessRule {
	t.Helper()
	return f.createAccessRuleWithScope(t, uuid.NullUUID{UUID: projectID, Valid: true}, "project", disposition, matchBreadth, matchValue)
}

func (f *fixture) createAccessRuleWithScope(t *testing.T, projectID uuid.NullUUID, accessScope string, disposition string, matchBreadth string, matchValue string) accessrepo.ShadowMcpAccessRule {
	t.Helper()
	rule, err := accessrepo.New(f.conn).CreateShadowMCPAccessRule(t.Context(), accessrepo.CreateShadowMCPAccessRuleParams{
		OrganizationID:         f.orgID,
		ProjectID:              projectID,
		AccessScope:            accessScope,
		Disposition:            disposition,
		MatchBreadth:           matchBreadth,
		MatchValue:             matchValue,
		DisplayName:            matchValue,
		ObservedFullUrl:        pgtype.Text{},
		ObservedUrlHost:        pgtype.Text{},
		ObservedServerIdentity: pgtype.Text{},
		SourceRequestID:        uuid.NullUUID{},
		CreatedBy:              pgtype.Text{},
		UpdatedBy:              pgtype.Text{},
		Reason:                 pgtype.Text{},
	})
	require.NoError(t, err)
	return rule
}
