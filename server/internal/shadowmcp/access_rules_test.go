package shadowmcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestEvaluateAccessRules_AllowsMatchingRuleWithGrant(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	rule := f.createAccessRule(t, "allowed", "full_url", "https://mcp.example.com/v1")
	authorizer := accessRuleAuthorizer{allowedRules: map[string]struct{}{rule.ID.String(): {}}}

	decision := f.client.EvaluateAccessRules(t.Context(), authorizer, f.orgID, f.projectID.String(), "user_test", shadowmcp.AccessEvidence{
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
	allowed := f.createAccessRule(t, "allowed", "url_host", "mcp.example.com")
	denied := f.createAccessRule(t, "denied", "full_url", "https://mcp.example.com/v1")
	authorizer := accessRuleAuthorizer{allowedRules: map[string]struct{}{allowed.ID.String(): {}}}

	decision := f.client.EvaluateAccessRules(t.Context(), authorizer, f.orgID, f.projectID.String(), "user_test", shadowmcp.AccessEvidence{
		FullURL:        "https://mcp.example.com/v1",
		URLHost:        "",
		ServerIdentity: "",
	})

	require.Equal(t, shadowmcp.AccessRuleOutcomeDenied, decision.Outcome)
	require.Equal(t, denied.ID.String(), decision.RuleID)
}

func TestEvaluateAccessRules_MissingGrantBlocksAllowedRule(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.createAccessRule(t, "allowed", "server_identity", "github")
	authorizer := accessRuleAuthorizer{allowedRules: map[string]struct{}{}}

	decision := f.client.EvaluateAccessRules(t.Context(), authorizer, f.orgID, f.projectID.String(), "user_test", shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: "github",
	})

	require.Equal(t, shadowmcp.AccessRuleOutcomeMissingGrant, decision.Outcome)
}

type accessRuleAuthorizer struct {
	allowedRules map[string]struct{}
}

func (a accessRuleAuthorizer) RequireShadowMCPConnect(_ context.Context, _ string, _ string, ruleID string, _ string) error {
	if _, ok := a.allowedRules[ruleID]; ok {
		return nil
	}
	return oops.C(oops.CodeForbidden)
}

func (f *fixture) createAccessRule(t *testing.T, disposition string, matchBreadth string, matchValue string) accessrepo.ShadowMcpAccessRule {
	t.Helper()
	rule, err := accessrepo.New(f.conn).CreateShadowMCPAccessRule(t.Context(), accessrepo.CreateShadowMCPAccessRuleParams{
		OrganizationID:         f.orgID,
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
