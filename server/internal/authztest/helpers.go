package authztest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// WithExactGrants marks the context as enterprise and loads the given grants
// directly into the context. Pass no grants to simulate RBAC active with no permissions.
func WithExactGrants(t *testing.T, ctx context.Context, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return authz.GrantsToContext(ctx, grants)
}

func RBACAlwaysEnabled(context.Context, string) (bool, error) {
	return true, nil
}

func RBACAlwaysDisabled(context.Context, string) (bool, error) {
	return false, nil
}

func ChallengeLoggingAlwaysDisabled(context.Context, string) (bool, error) {
	return false, nil
}

func ChallengeLoggingAlwaysEnabled(context.Context, string) (bool, error) {
	return true, nil
}
