package authz

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAdditionalRestrictionCannotAuthorizeAndSurvivesAdmission(t *testing.T) {
	t.Parallel()
	grant := NewGrant(ScopeMCPConnect, "server")
	check := MCPToolCallCheck("server", MCPToolCallDimensions{Tool: "read"})
	ctx := RestrictContext(context.Background(), []Grant{grant})
	_, ok := grantAuthorizationFromContext(ctx)
	require.False(t, ok, "restriction is not admission")
	ctx = admittedPoliciesToContext(ctx, []Grant{grant}, []Grant{grant})
	policy, ok := grantAuthorizationFromContext(ctx)
	require.True(t, ok)
	result, err := policy.evaluate(check)
	require.NoError(t, err)
	require.NotNil(t, result.Grant)
	ctx = RestrictContext(ctx, nil)
	ctx = admittedPoliciesToContext(ctx, []Grant{grant}, []Grant{grant})
	policy, ok = grantAuthorizationFromContext(ctx)
	require.True(t, ok)
	result, err = policy.evaluate(check)
	require.NoError(t, err)
	require.Nil(t, result.Grant, "readmission cannot discard human restrictions")
}
