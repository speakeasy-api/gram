package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestSetUserSessionIssuer_AttachesOrganizationIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         "Organization authenticated toolset",
		ToolUrns:     []string{},
		ResourceUrns: []string{},
	})
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:                         "shared-workforce",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	issuerID := issuer.ID.String()

	updated, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{
		Slug:                toolset.Slug,
		UserSessionIssuerID: &issuerID,
	})
	require.NoError(t, err)
	require.Equal(t, issuerID, *updated.UserSessionIssuerID)
}
