package authztest

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// WithExactGrants loads the given grants directly into the context. Pass no
// grants to simulate RBAC active with no permissions.
func WithExactGrants(t *testing.T, ctx context.Context, grants ...authz.Grant) context.Context {
	t.Helper()

	normalized := append([]authz.Grant(nil), grants...)
	for i := range normalized {
		if normalized[i].Effect == "" {
			normalized[i].Effect = authz.PolicyEffectAllow
		}
	}

	return authz.GrantsToContext(ctx, normalized)
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
