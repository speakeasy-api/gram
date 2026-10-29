package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestPolicySet_BindsPoliciesToTheirOrganization(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())

	loaded := withPolicyOrganization(GrantsToContext(enterpriseSessionCtxWithOrg(t, "org_loaded"), []Grant{NewGrant(ScopeRoot, WildcardResource)}))
	set, err := engine.PolicySet(loaded)
	require.NoError(t, err)
	require.True(t, set.Enforced)
	require.Equal(t, "org_loaded", set.OrganizationID)
	require.Len(t, set.Policies, 1)

	// The request switched organizations without reloading its policies.
	authCtx, ok := contextvalues.GetAuthContext(loaded)
	require.True(t, ok)
	switched := *authCtx
	switched.ActiveOrganizationID = "org_switched"
	set, err = engine.PolicySet(contextvalues.SetAuthContext(loaded, &switched))
	require.NoError(t, err)
	require.Empty(t, set.OrganizationID, "a contradicted binding binds nothing")

	admitted := admittedPoliciesToContext(enterpriseSessionCtxWithOrg(t, "org_loaded"), RepresentedUserPolicy{UserID: "", Policy: nil}, false,
		[]Grant{NewGrant(ScopeRoot, WildcardResource)}, []Grant{NewGrant(ScopeMCPConnect, WildcardResource)})
	set, err = engine.PolicySet(admitted)
	require.NoError(t, err)
	require.Equal(t, "org_loaded", set.OrganizationID)
	require.Len(t, set.Policies, 2)
}
