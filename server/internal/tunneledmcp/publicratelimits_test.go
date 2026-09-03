package tunneledmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/publiclimits"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

func updateLimits(id uuid.UUID, requestRate, requestBurst *int) *gen.UpdateServerPayload {
	return &gen.UpdateServerPayload{
		SessionToken:               nil,
		ApikeyToken:                nil,
		ProjectSlugInput:           nil,
		ID:                         id.String(),
		Name:                       nil,
		AllowPublic:                nil,
		ResourceIdentifier:         nil,
		PublicRequestRatePerSecond: requestRate,
		PublicRequestBurst:         requestBurst,
	}
}

func createServerForLimits(t *testing.T, ctx context.Context, ti *testInstance, name string) (context.Context, uuid.UUID) {
	t.Helper()
	authCtx := requireAuthContext(t, ctx)
	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))
	result, err := ti.service.CreateServer(writeCtx, &gen.CreateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Name:               name,
		ResourceIdentifier: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Server)
	return writeCtx, uuid.MustParse(result.Server.ID)
}

func TestUpdateServer_PublicRateLimits(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	writeCtx, id := createServerForLimits(t, ctx, ti, "limits")

	// Fresh rows carry no limits: the deployment defaults apply.
	view, err := ti.service.GetServer(writeCtx, &gen.GetServerPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: id.String()})
	require.NoError(t, err)
	require.Nil(t, view.PublicRequestRatePerSecond)
	require.Nil(t, view.PublicRequestBurst)
	require.Equal(t, publiclimits.DefaultRequestRatePerSecond, view.EffectivePublicRequestRatePerSecond)
	require.Equal(t, publiclimits.DefaultRequestBurst, view.EffectivePublicRequestBurst)

	view, err = ti.service.UpdateServer(writeCtx, updateLimits(id, new(300), nil))
	require.NoError(t, err)
	require.Equal(t, 300, *view.PublicRequestRatePerSecond)
	require.Nil(t, view.PublicRequestBurst, "omitted burst stays unset (twice the rate applies at serve time)")
	require.Equal(t, 300, view.EffectivePublicRequestRatePerSecond)
	require.Equal(t, 600, view.EffectivePublicRequestBurst)

	view, err = ti.service.UpdateServer(writeCtx, updateLimits(id, nil, new(450)))
	require.NoError(t, err)
	require.Equal(t, 300, *view.PublicRequestRatePerSecond, "omitted rate is left alone")
	require.Equal(t, 450, *view.PublicRequestBurst)
	require.Equal(t, 450, view.EffectivePublicRequestBurst)

	// Omitted fields leave stored values alone; 0 clears back to the default.
	view, err = ti.service.UpdateServer(writeCtx, updateLimits(id, new(0), nil))
	require.NoError(t, err)
	require.Nil(t, view.PublicRequestRatePerSecond)
	require.Equal(t, 450, *view.PublicRequestBurst)
	require.Equal(t, publiclimits.DefaultRequestRatePerSecond, view.EffectivePublicRequestRatePerSecond, "no stored rate: defaults apply even with a stray stored burst")
	require.Equal(t, publiclimits.DefaultRequestBurst, view.EffectivePublicRequestBurst)

	row, err := repo.New(ti.conn).GetServerByID(ctx, repo.GetServerByIDParams{ID: id, ProjectID: *requireAuthContext(t, ctx).ProjectID})
	require.NoError(t, err)
	require.False(t, row.PublicRequestRatePerSecond.Valid)
	require.Equal(t, int32(450), row.PublicRequestBurst.Int32)
}

func TestUpdateServer_PublicRateLimits_Validation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	writeCtx, id := createServerForLimits(t, ctx, ti, "limits-validation")

	_, err := ti.service.UpdateServer(writeCtx, updateLimits(id, new(-1), nil))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.UpdateServer(writeCtx, updateLimits(id, new(publiclimits.MaxRequestRatePerSecond+1), nil))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.UpdateServer(writeCtx, updateLimits(id, nil, new(publiclimits.MaxRequestBurst+1)))
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Read-only access cannot change limits.
	authCtx := requireAuthContext(t, ctx)
	readCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPRead, *authCtx.ProjectID))
	_, err = ti.service.UpdateServer(readCtx, updateLimits(id, new(10), nil))
	requireOopsCode(t, err, oops.CodeForbidden)
}
