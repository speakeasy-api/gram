package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// The identity page lists the approvals one person asked for, so the filter
// has to match on the requester recorded at redemption time and exclude
// everyone else's requests.
func TestListRiskPolicyBypassRequests_FiltersByRequester(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Requester Filter")})
	require.NoError(t, err)

	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, "https://mcp.example.com/requester-filter"),
	})
	require.NoError(t, err)
	require.Equal(t, policy.ID, redeemedBypassRow(t, ctx, ti, request).PolicyID)

	mine, err := ti.service.ListRiskPolicyBypassRequests(ctx, &gen.ListRiskPolicyBypassRequestsPayload{
		RequesterUserIds: []string{authCtx.UserID},
	})
	require.NoError(t, err)
	require.Len(t, mine.Requests, 1)

	theirs, err := ti.service.ListRiskPolicyBypassRequests(ctx, &gen.ListRiskPolicyBypassRequestsPayload{
		RequesterUserIds: []string{"user_someone_else"},
	})
	require.NoError(t, err)
	require.Empty(t, theirs.Requests)

	unfiltered, err := ti.service.ListRiskPolicyBypassRequests(ctx, &gen.ListRiskPolicyBypassRequestsPayload{})
	require.NoError(t, err)
	require.Len(t, unfiltered.Requests, 1)
}
