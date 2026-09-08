package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// The substring user_id filter routinely pulls in other people: "dev@acme.com"
// contains "dev@acme.co". The identity page needs exactly one subject, so
// external_user_ids matches whole ids, and takes a set because one person is
// recorded under several.
func TestListRiskResults_ExternalUserIDsMatchWholeIdentifiers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("External User Filter")})
	require.NoError(t, err)
	policyID := uuid.MustParse(policy.ID)

	_, subjectMsg := seedChatMessageWithUser(t, ti, projectID, orgID, "dev@acme.co")
	seedRiskResult(t, ti, projectID, orgID, policyID, 1, subjectMsg, true)

	// Contains the subject's whole id as a prefix: the substring filter cannot
	// tell these two apart.
	_, lookalikeMsg := seedChatMessageWithUser(t, ti, projectID, orgID, "dev@acme.com")
	seedRiskResult(t, ti, projectID, orgID, policyID, 2, lookalikeMsg, true)

	// The same person's second identifier.
	_, aliasMsg := seedChatMessageWithUser(t, ti, projectID, orgID, "personal@example.test")
	seedRiskResult(t, ti, projectID, orgID, policyID, 3, aliasMsg, true)

	substring, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID: &policy.ID,
		UserID:   new("dev@acme.co"),
	})
	require.NoError(t, err)
	require.Len(t, substring.Results, 2, "the substring filter also matches the lookalike")

	exact, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID:        &policy.ID,
		ExternalUserIds: []string{"dev@acme.co", "personal@example.test"},
	})
	require.NoError(t, err)
	require.Len(t, exact.Results, 2, "both of the subject's identifiers match, and the lookalike does not")

	// The two ids that the substring filter conflates resolve to different
	// findings under whole-id matching.
	subjectOnly, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID:        &policy.ID,
		ExternalUserIds: []string{"dev@acme.co"},
	})
	require.NoError(t, err)
	require.Len(t, subjectOnly.Results, 1)

	lookalikeOnly, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID:        &policy.ID,
		ExternalUserIds: []string{"dev@acme.com"},
	})
	require.NoError(t, err)
	require.Len(t, lookalikeOnly.Results, 1)
	require.NotEqual(t, subjectOnly.Results[0].ID, lookalikeOnly.Results[0].ID)
}

// An empty filter must not narrow the listing.
func TestListRiskResults_ExternalUserIDsEmptyIsUnnarrowed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Unfiltered")})
	require.NoError(t, err)
	policyID := uuid.MustParse(policy.ID)

	_, firstMsg := seedChatMessageWithUser(t, ti, projectID, orgID, "one@example.test")
	seedRiskResult(t, ti, projectID, orgID, policyID, 1, firstMsg, true)
	_, secondMsg := seedChatMessageWithUser(t, ti, projectID, orgID, "two@example.test")
	seedRiskResult(t, ti, projectID, orgID, policyID, 2, secondMsg, true)

	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID:        &policy.ID,
		ExternalUserIds: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
}
