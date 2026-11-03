package authztest

import (
	"context"
	"sync"
	"testing"
	"time"

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
	grants := make([]authz.Grant, 0, len(roleGrants)+len(additional))
	for _, grant := range roleGrants {
		grants = append(grants, authz.NewGrant(authz.Scope(grant.Scope), authz.WildcardResource))
	}
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

// NewClient builds the typed authorization client over engine with the
// default registry, failing the test if the registry does not compile. Like
// production with every migration flag off, the engine decides every call.
func NewClient(t *testing.T, engine *authz.Engine) *authz.Client {
	t.Helper()

	client, err := authz.NewDefaultClient(engine)
	if err != nil {
		t.Fatalf("create authorization client: %v", err)
	}
	return client
}

// NewClientInMode builds the default client with every default migration
// unit routed to mode, so a migrated service's tests can run against the
// engine, a shadow comparison, and the registry evaluator alike. As in
// production, a context with no active organization always runs old, so a
// test that wants the shadow or new path must authenticate an organization.
func NewClientInMode(t *testing.T, engine *authz.Engine, mode authz.MigrationMode) *authz.Client {
	t.Helper()

	registry, err := authz.DefaultRegistry()
	if err != nil {
		t.Fatalf("compile authorization registry: %v", err)
	}
	router, err := authz.NewMigrationRouter(registry, authz.FixedModes(mode), authz.DefaultMigrationUnits()...)
	if err != nil {
		t.Fatalf("create authorization migration router: %v", err)
	}
	client, err := authz.NewClient(engine, registry, authz.WithMigrationRouter(router))
	if err != nil {
		t.Fatalf("create authorization client: %v", err)
	}
	return client
}

// NewParityClient builds the default client with every default migration unit
// in shadow mode and a recorder that fails the test on any disagreement or
// unfinished comparison. The engine still answers every call, so a service's
// existing assertions hold, and each organization-bound decision the tests
// make is also checked against the registry evaluator.
func NewParityClient(t *testing.T, engine *authz.Engine) *authz.Client {
	t.Helper()

	registry, err := authz.DefaultRegistry()
	if err != nil {
		t.Fatalf("compile authorization registry: %v", err)
	}
	router, err := authz.NewMigrationRouter(registry, authz.FixedModes(authz.MigrationModeShadow), authz.DefaultMigrationUnits()...)
	if err != nil {
		t.Fatalf("create authorization migration router: %v", err)
	}
	client, err := authz.NewClient(engine, registry,
		authz.WithMigrationRouter(router),
		authz.WithShadowRecorder(&parityRecorder{t: t, mu: sync.Mutex{}}),
		authz.WithShadowBudget(authz.ShadowBudget{MaxConcurrent: 1024, Timeout: 5 * time.Second}),
	)
	if err != nil {
		t.Fatalf("create authorization client: %v", err)
	}
	return client
}

type parityRecorder struct {
	t  *testing.T
	mu sync.Mutex
}

func (r *parityRecorder) RecordShadowComparison(_ context.Context, c authz.ShadowComparison) {
	if !c.Outcome.Disagrees() && c.Outcome != authz.ShadowFailed && c.Outcome != authz.ShadowSkipped {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t.Errorf("authorization parity: %s %v on units %v gave %s (%s)", c.Entry, c.Permissions, c.Units, c.Outcome, c.Detail)
}
