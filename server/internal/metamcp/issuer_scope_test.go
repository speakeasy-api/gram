package metamcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func seedOrganizationIssuer(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) uuid.UUID {
	t.Helper()
	id, err := testrepo.New(ti.conn).InsertOrganizationTierUserSessionIssuerFixture(ctx, testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{
		OrganizationID:     conv.ToPGText(organizationID),
		Slug:               "org-issuer-" + uuid.NewString(),
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	return id
}

func TestMetaMcpServer_OrganizationIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		Name: "organization issuer gateway", UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, conv.PtrEmpty(issuerID.String()), created.UserSessionIssuerID)

	// An instructions-only edit must validate and preserve the stored org issuer.
	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		ID: created.ID, Name: created.Name, Instructions: conv.PtrEmpty("Call list_servers first."),
	})
	require.NoError(t, err)
	require.Equal(t, created.UserSessionIssuerID, updated.UserSessionIssuerID)
	require.Equal(t, conv.PtrEmpty("Call list_servers first."), updated.Instructions)
	fetched, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, updated.UserSessionIssuerID, fetched.UserSessionIssuerID)
	require.Equal(t, updated.Instructions, fetched.Instructions)

	target, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "attach organization issuer"})
	require.NoError(t, err)
	attached, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		ID: target.ID, Name: target.Name, UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, created.UserSessionIssuerID, attached.UserSessionIssuerID)
}

func TestMetaMcpServer_RejectsInvalidIssuerScope(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"deleted organization", "foreign organization", "other project in same organization"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			var issuerID uuid.UUID
			switch scope {
			case "deleted organization":
				issuerID = seedOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
				_, err := usersessionsrepo.New(ti.conn).DeleteOrganizationUserSessionIssuer(ctx, usersessionsrepo.DeleteOrganizationUserSessionIssuerParams{
					ID: issuerID, OrganizationID: authCtx.ActiveOrganizationID,
				})
				require.NoError(t, err)
			case "foreign organization":
				organizationID := "org_issuer_scope_" + uuid.NewString()
				require.NoError(t, testrepo.New(ti.conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
					ID: organizationID, Name: "Other test organization", Slug: "other-" + uuid.NewString(), GramAccountType: "enterprise",
					FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()),
					FreeTrialEndsAt:    conv.ToPGTimestamptz(time.Now().Add(14 * 24 * time.Hour)),
				}))
				issuerID = seedOrganizationIssuer(t, ctx, ti, organizationID)
			case "other project in same organization":
				projectID := seedOtherProject(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
				issuerID = seedUserSessionIssuer(t, ctx, ti.conn, projectID)
			}

			_, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
				Name: "invalid issuer gateway", UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
			})
			requireOopsCode(t, err, oops.CodeInvalid)
			require.ErrorContains(t, err, "live issuer available to this project")

			created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "unchanged gateway"})
			require.NoError(t, err)
			_, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
				ID: created.ID, Name: "must not persist", UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
			})
			requireOopsCode(t, err, oops.CodeInvalid)
			require.ErrorContains(t, err, "live issuer available to this project")
			fetched, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{ID: created.ID})
			require.NoError(t, err)
			require.Equal(t, created.Name, fetched.Name)
			require.Equal(t, created.UserSessionIssuerID, fetched.UserSessionIssuerID)
		})
	}
}
