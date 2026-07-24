package authztest

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// RBACState is a concurrency-safe feature checker and enabler for tests.
type RBACState struct {
	enabled atomic.Bool
}

// NewRBACState creates test RBAC state with the requested initial value.
func NewRBACState(enabled bool) *RBACState {
	state := new(RBACState)
	state.enabled.Store(enabled)
	return state
}

// IsEnabled reports the current test RBAC state.
func (s *RBACState) IsEnabled(context.Context, string) (bool, error) {
	return s.enabled.Load(), nil
}

// EnableRBAC changes the test RBAC state to enabled.
func (s *RBACState) EnableRBAC(context.Context, string) error {
	s.enabled.Store(true)
	return nil
}

// WithExactGrants marks the context as enterprise and loads the given grants
// directly into the context. Pass no grants to simulate RBAC active with no permissions.
func WithExactGrants(t *testing.T, ctx context.Context, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

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
