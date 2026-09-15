// scope_override three-state semantics at the handler level, project and org tiers.

package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// storedScopeOverride reads the column back from the row, since the API
// result alone cannot tell NULL from a decoding artefact.
func storedScopeOverride(t *testing.T, ctx context.Context, ti *testInstance, id string) []string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := repo.New(ti.conn).GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:                    uuid.MustParse(id),
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeOrganizational: true,
		IncludeGlobal:         false,
	})
	require.NoError(t, err)
	return row.ScopeOverride
}

// scopeOverrideTier abstracts the create + update entry points of one tenancy
// tier so the same table runs against both.
type scopeOverrideTier struct {
	name   string
	create func(ctx context.Context, ti *testInstance, slug string, override []string) (*types.RemoteSessionIssuer, error)
	update func(ctx context.Context, ti *testInstance, id string, override []string) (*types.RemoteSessionIssuer, error)
}

func scopeOverrideTiers() []scopeOverrideTier {
	return []scopeOverrideTier{
		{
			name: "project",
			create: func(ctx context.Context, ti *testInstance, slug string, override []string) (*types.RemoteSessionIssuer, error) {
				payload := newIssuerPayload(slug)
				payload.ScopeOverride = override
				return ti.service.CreateRemoteSessionIssuer(ctx, payload)
			},
			update: func(ctx context.Context, ti *testInstance, id string, override []string) (*types.RemoteSessionIssuer, error) {
				return ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
					ID:            id,
					ScopeOverride: override,
				})
			},
		},
		{
			name: "organization",
			create: func(ctx context.Context, ti *testInstance, slug string, override []string) (*types.RemoteSessionIssuer, error) {
				payload := newCreateIssuerPayload(slug, nil)
				payload.ScopeOverride = override
				return ti.service.CreateIssuer(ctx, payload)
			},
			update: func(ctx context.Context, ti *testInstance, id string, override []string) (*types.RemoteSessionIssuer, error) {
				return ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{
					ID:            id,
					ScopeOverride: override,
				})
			},
		},
	}
}

func TestIssuerScopeOverride_CreateWithEmptyArrayStoresNull(t *testing.T) {
	t.Parallel()

	for _, tier := range scopeOverrideTiers() {
		t.Run(tier.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)

			created, err := tier.create(ctx, ti, "scope-override-empty-"+tier.name, []string{})
			require.NoError(t, err)
			require.Nil(t, created.ScopeOverride)
			require.Nil(t, storedScopeOverride(t, ctx, ti, created.ID))

			omitted, err := tier.create(ctx, ti, "scope-override-omitted-"+tier.name, nil)
			require.NoError(t, err)
			require.Nil(t, omitted.ScopeOverride)
			require.Nil(t, storedScopeOverride(t, ctx, ti, omitted.ID))

			set, err := tier.create(ctx, ti, "scope-override-set-"+tier.name, []string{"custom:one", "custom:two"})
			require.NoError(t, err)
			require.Equal(t, []string{"custom:one", "custom:two"}, set.ScopeOverride)
			require.Equal(t, []string{"custom:one", "custom:two"}, storedScopeOverride(t, ctx, ti, set.ID))
		})
	}
}

func TestIssuerScopeOverride_UpdateWithEmptyArrayClears(t *testing.T) {
	t.Parallel()

	for _, tier := range scopeOverrideTiers() {
		t.Run(tier.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)

			created, err := tier.create(ctx, ti, "scope-override-clear-"+tier.name, []string{"custom:one"})
			require.NoError(t, err)
			require.Equal(t, []string{"custom:one"}, created.ScopeOverride)

			cleared, err := tier.update(ctx, ti, created.ID, []string{})
			require.NoError(t, err)
			require.Nil(t, cleared.ScopeOverride)
			require.Nil(t, storedScopeOverride(t, ctx, ti, created.ID))
		})
	}
}

func TestIssuerScopeOverride_UpdateOmittedKeeps(t *testing.T) {
	t.Parallel()

	for _, tier := range scopeOverrideTiers() {
		t.Run(tier.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)

			created, err := tier.create(ctx, ti, "scope-override-keep-"+tier.name, []string{"custom:one"})
			require.NoError(t, err)

			kept, err := tier.update(ctx, ti, created.ID, nil)
			require.NoError(t, err)
			require.Equal(t, []string{"custom:one"}, kept.ScopeOverride)
			require.Equal(t, []string{"custom:one"}, storedScopeOverride(t, ctx, ti, created.ID))

			replaced, err := tier.update(ctx, ti, created.ID, []string{"custom:two"})
			require.NoError(t, err)
			require.Equal(t, []string{"custom:two"}, replaced.ScopeOverride)
			require.Equal(t, []string{"custom:two"}, storedScopeOverride(t, ctx, ti, created.ID))
		})
	}
}
