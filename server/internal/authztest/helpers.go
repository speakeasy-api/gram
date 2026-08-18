package authztest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// InitAuthContext creates the standard authenticated test context and gives
// its user the built-in Admin role grants, matching a newly created org.
func InitAuthContext(t *testing.T, ctx context.Context, conn *pgxpool.Pool, sessionManager *sessions.Manager) context.Context {
	t.Helper()

	return WithAdminGrants(testenv.InitAuthContext(t, ctx, conn, sessionManager))
}

// WithAdminGrants replaces the prepared grants with the built-in Admin role
// plus any extra grants required by the fixture.
func WithAdminGrants(ctx context.Context, additional ...authz.Grant) context.Context {
	roleGrants := authz.SystemRoleGrants[authz.SystemRoleAdmin]
	overrides := make([]authz.RoleGrant, 0, len(roleGrants))
	for _, grant := range roleGrants {
		overrides = append(overrides, *grant)
	}

	grants := authz.GrantsFromOverrides(overrides)
	return authz.GrantsToContext(ctx, append(grants, additional...))
}

// WithExactGrants loads the given grants directly into the context. Pass no
// grants to simulate RBAC active with no permissions.
func WithExactGrants(t *testing.T, ctx context.Context, grants ...authz.Grant) context.Context {
	t.Helper()
	return authz.GrantsToContext(ctx, grants)
}

func ChallengeLoggingAlwaysDisabled(context.Context, string) (bool, error) {
	return false, nil
}

func ChallengeLoggingAlwaysEnabled(context.Context, string) (bool, error) {
	return true, nil
}
