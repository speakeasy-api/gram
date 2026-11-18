package toolsets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func createProjectIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug string) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		OrganizationID:     pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func linkHostedIssuer(t *testing.T, ctx context.Context, ti *testInstance, toolset *types.Toolset, issuerID uuid.UUID) {
	t.Helper()
	_, err := ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "private", func(u *mcpservers.HostedServerUpdate) {
		u.UserSessionIssuerID = uuid.NullUUID{UUID: issuerID, Valid: true}
	}))
	require.NoError(t, err)
}

func issuerLive(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) bool {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:             issuerID,
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
	issuerID := createProjectIssuer(t, ctx, ti, "released-"+uuid.NewString()[:8])
	linkHostedIssuer(t, ctx, ti, toolset, issuerID)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug}))

	require.False(t, issuerLive(t, ctx, ti, issuerID))
}

// An issuer another hosted server still uses survives the toolset's deletion.
func TestDeleteToolsetKeepsAnIssuerAnotherServerUses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	deleted := createMinimalPrivateToolset(t, ctx, ti, "Deleted issuer sharer")
	kept := createMinimalPrivateToolset(t, ctx, ti, "Kept issuer sharer")
	issuerID := createProjectIssuer(t, ctx, ti, "shared-"+uuid.NewString()[:8])
	for _, toolset := range []*types.Toolset{deleted, kept} {
		linkHostedIssuer(t, ctx, ti, toolset, issuerID)
	}

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: deleted.Slug}))

	require.True(t, issuerLive(t, ctx, ti, issuerID))
}
