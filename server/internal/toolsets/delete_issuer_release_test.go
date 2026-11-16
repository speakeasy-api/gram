package toolsets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func shimIssuerLive(t *testing.T, ctx context.Context, ti *testInstance, issuerID string) bool {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:             uuid.MustParse(issuerID),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	require.NoError(t, err)
	return true
}

// Deleting a toolset deletes its hosted server, which, like any deleted MCP
// server, takes along an issuer nothing else uses.
func TestDeleteToolsetReleasesAnIssuerOnlyItsHostedServerUsed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Released issuer toolset")
	issuerID := createShimIssuer(t, ctx, ti, "released-"+uuid.NewString()[:8])
	_, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &issuerID})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug}))

	require.False(t, shimIssuerLive(t, ctx, ti, issuerID))
}

// An issuer another hosted server still uses survives the toolset's deletion.
func TestDeleteToolsetKeepsAnIssuerAnotherServerUses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	deleted := createMinimalPrivateToolset(t, ctx, ti, "Deleted issuer sharer")
	kept := createMinimalPrivateToolset(t, ctx, ti, "Kept issuer sharer")
	issuerID := createShimIssuer(t, ctx, ti, "shared-"+uuid.NewString()[:8])
	for _, slug := range []types.Slug{deleted.Slug, kept.Slug} {
		_, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: slug, UserSessionIssuerID: &issuerID})
		require.NoError(t, err)
	}

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: deleted.Slug}))

	require.True(t, shimIssuerLive(t, ctx, ti, issuerID))
}
