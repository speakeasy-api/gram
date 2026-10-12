package triggers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/triggers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestTriggerServiceRequiresProjectScopes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	readCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err := ti.service.ListTriggerInstances(readCtx, &gen.ListTriggerInstancesPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.CreateTriggerInstance(readCtx, newCreatePayload(ti.environmentID, "denied"))
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)

	assistantCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeAssistantWrite, authCtx.ProjectID.String()))
	_, err = ti.service.ListTriggerInstances(assistantCtx, &gen.ListTriggerInstancesPayload{SessionToken: nil, ProjectSlugInput: nil})
	shareableErr = nil
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}
