// omit_scope_fallback tri-state semantics at the handler level, all three
// issuer tiers.

package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// storedOmitScopeFallback reads the column back from a tenant row, since the
// API result alone cannot tell NULL from a decoding artefact.
func storedOmitScopeFallback(t *testing.T, ctx context.Context, ti *testInstance, id string) pgtype.Bool {
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
	return row.OmitScopeFallback
}

func storedGlobalOmitScopeFallback(t *testing.T, ctx context.Context, ti *testInstance, id string) pgtype.Bool {
	t.Helper()
	row, err := repo.New(ti.conn).GetGlobalRemoteSessionIssuerByID(ctx, uuid.MustParse(id))
	require.NoError(t, err)
	return row.OmitScopeFallback
}

// omitScopeFallbackTier abstracts one tenancy tier's create, update and
// stored-value read so the same assertions run against all three.
type omitScopeFallbackTier struct {
	name   string
	ctx    func(t *testing.T, ctx context.Context) context.Context
	create func(t *testing.T, ctx context.Context, ti *testInstance, slug string, omit *bool) (*types.RemoteSessionIssuer, error)
	update func(ctx context.Context, ti *testInstance, id string, omit *bool) (*types.RemoteSessionIssuer, error)
	stored func(t *testing.T, ctx context.Context, ti *testInstance, id string) pgtype.Bool
}

func omitScopeFallbackTiers() []omitScopeFallbackTier {
	sameCtx := func(_ *testing.T, ctx context.Context) context.Context { return ctx }
	return []omitScopeFallbackTier{
		{
			name: "project",
			ctx:  sameCtx,
			create: func(t *testing.T, ctx context.Context, ti *testInstance, slug string, omit *bool) (*types.RemoteSessionIssuer, error) {
				t.Helper()
				payload := newIssuerPayload(slug)
				payload.OmitScopeFallback = omit
				return ti.service.CreateRemoteSessionIssuer(ctx, payload)
			},
			update: func(ctx context.Context, ti *testInstance, id string, omit *bool) (*types.RemoteSessionIssuer, error) {
				return ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
					ID:                id,
					OmitScopeFallback: omit,
				})
			},
			stored: storedOmitScopeFallback,
		},
		{
			name: "organization",
			ctx:  sameCtx,
			create: func(t *testing.T, ctx context.Context, ti *testInstance, slug string, omit *bool) (*types.RemoteSessionIssuer, error) {
				t.Helper()
				payload := newCreateIssuerPayload(slug, nil)
				payload.OmitScopeFallback = omit
				return ti.service.CreateIssuer(ctx, payload)
			},
			update: func(ctx context.Context, ti *testInstance, id string, omit *bool) (*types.RemoteSessionIssuer, error) {
				return ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{
					ID:                id,
					OmitScopeFallback: omit,
				})
			},
			stored: storedOmitScopeFallback,
		},
		{
			name: "platform",
			ctx:  withAdmin,
			create: func(t *testing.T, ctx context.Context, ti *testInstance, slug string, omit *bool) (*types.RemoteSessionIssuer, error) {
				t.Helper()
				payload := createGlobalIssuer(t, slug)
				payload.OmitScopeFallback = omit
				return ti.service.CreateGlobalIssuer(ctx, payload)
			},
			update: func(ctx context.Context, ti *testInstance, id string, omit *bool) (*types.RemoteSessionIssuer, error) {
				return ti.service.UpdateGlobalIssuer(ctx, &adminrsgen.UpdateGlobalIssuerPayload{
					ID:                id,
					OmitScopeFallback: omit,
				})
			},
			stored: storedGlobalOmitScopeFallback,
		},
	}
}

func TestIssuerOmitScopeFallback_CreateOmittedStoresNull(t *testing.T) {
	t.Parallel()

	for _, tier := range omitScopeFallbackTiers() {
		t.Run(tier.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ctx = tier.ctx(t, ctx)

			omitted, err := tier.create(t, ctx, ti, "omit-fallback-omitted-"+tier.name, nil)
			require.NoError(t, err)
			require.Nil(t, omitted.OmitScopeFallback)
			require.False(t, tier.stored(t, ctx, ti, omitted.ID).Valid)

			set, err := tier.create(t, ctx, ti, "omit-fallback-set-"+tier.name, new(true))
			require.NoError(t, err)
			require.Equal(t, new(true), set.OmitScopeFallback)
			require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, tier.stored(t, ctx, ti, set.ID))
		})
	}
}

func TestIssuerOmitScopeFallback_UpdateOmittedKeepsExplicitSets(t *testing.T) {
	t.Parallel()

	for _, tier := range omitScopeFallbackTiers() {
		t.Run(tier.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ctx = tier.ctx(t, ctx)

			created, err := tier.create(t, ctx, ti, "omit-fallback-update-"+tier.name, new(true))
			require.NoError(t, err)

			kept, err := tier.update(ctx, ti, created.ID, nil)
			require.NoError(t, err)
			require.Equal(t, new(true), kept.OmitScopeFallback)
			require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, tier.stored(t, ctx, ti, created.ID))

			cleared, err := tier.update(ctx, ti, created.ID, new(false))
			require.NoError(t, err)
			require.Equal(t, new(false), cleared.OmitScopeFallback)
			require.Equal(t, pgtype.Bool{Bool: false, Valid: true}, tier.stored(t, ctx, ti, created.ID))
		})
	}
}
