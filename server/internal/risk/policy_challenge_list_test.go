package risk_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// seedChallenge records one warn/challenge against a user identifier.
func seedChallenge(t *testing.T, ctx context.Context, conn riskrepo.DBTX, orgID string, projectID uuid.UUID, policyID uuid.UUID, userID string, toolName string) riskrepo.RiskPolicyChallenge {
	t.Helper()

	row, err := riskrepo.New(conn).UpsertRiskPolicyChallenge(ctx, riskrepo.UpsertRiskPolicyChallengeParams{
		ID:              uuid.Must(uuid.NewV7()),
		OrganizationID:  orgID,
		ProjectID:       projectID,
		RiskPolicyID:    policyID,
		UserID:          userID,
		ToolName:        conv.ToPGTextEmpty(toolName),
		CallFingerprint: conv.ToPGTextEmpty(""),
		PolicyName:      conv.ToPGTextEmpty("Warn Policy"),
		Entity:          conv.ToPGTextEmpty("secret"),
		RuleID:          conv.ToPGTextEmpty("secret.aws_key"),
	})
	require.NoError(t, err)

	return row
}

// A subject is commonly known by several identifiers, and the one recorded on
// a challenge is whichever the agent reported. Passing the whole set has to
// return every challenge across it, and nobody else's.
func TestListRiskPolicyChallenges_MatchesAnyUserIdentifier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Warn Policy")})
	require.NoError(t, err)
	policyID := uuid.MustParse(policy.ID)

	seedChallenge(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, policyID, "subject@example.com", "read_file")
	seedChallenge(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, policyID, "subject-external-id", "write_file")
	seedChallenge(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, policyID, "someone-else@example.com", "read_file")

	result, err := ti.service.ListRiskPolicyChallenges(ctx, &gen.ListRiskPolicyChallengesPayload{
		UserIds:          []string{"subject@example.com", "subject-external-id"},
		Status:           nil,
		Limit:            nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Challenges, 2)

	users := make([]string, 0, len(result.Challenges))
	for _, challenge := range result.Challenges {
		users = append(users, challenge.UserID)
		require.Equal(t, "challenged", challenge.Status)
		require.Equal(t, policy.ID, challenge.RiskPolicyID)
	}
	require.ElementsMatch(t, []string{"subject@example.com", "subject-external-id"}, users)
}

func TestListRiskPolicyChallenges_RequiresUserIdentifier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.ListRiskPolicyChallenges(ctx, &gen.ListRiskPolicyChallengesPayload{
		UserIds:          []string{"   "},
		Status:           nil,
		Limit:            nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
}

func TestListRiskPolicyChallenges_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.ListRiskPolicyChallenges(ctx, &gen.ListRiskPolicyChallengesPayload{
		UserIds:          []string{"subject@example.com"},
		Status:           nil,
		Limit:            nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}
