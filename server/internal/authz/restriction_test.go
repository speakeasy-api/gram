package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdditionalRestrictionCannotAuthorizeAndSurvivesAdmission(t *testing.T) {
	t.Parallel()
	grant := NewGrant(ScopeMCPConnect, "server")
	check := MCPToolCallCheck("server", MCPToolCallDimensions{Tool: "read"})
	restriction := NewGrant(ScopeMCPConnect, "other-server")
	ctx := RestrictContext(context.Background(), []Grant{restriction})
	_, ok := grantAuthorizationFromContext(ctx)
	require.False(t, ok, "restriction is not admission")
	ctx = admittedPoliciesToContext(ctx, []Grant{grant}, []Grant{grant})
	policy, ok := grantAuthorizationFromContext(ctx)
	require.True(t, ok)
	result, err := policy.evaluate(check)
	require.NoError(t, err)
	require.Nil(t, result.Grant, "restriction denies resource allowed by admission")
	ctx = RestrictContext(ctx, []Grant{grant})
	ctx = admittedPoliciesToContext(ctx, []Grant{grant}, []Grant{grant})
	policy, ok = grantAuthorizationFromContext(ctx)
	require.True(t, ok)
	result, err = policy.evaluate(check)
	require.NoError(t, err)
	require.Nil(t, result.Grant, "readmission cannot discard human restrictions")
}
