package mcp_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/bindings"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestKitIssuerLookupRejectsUnscopedAndForeignIssuers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	otherOrg := "org-" + uuid.NewString()
	require.NoError(t, testrepo.New(ti.conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{ID: otherOrg, Name: "Other organization", Slug: "other-" + uuid.NewString(), GramAccountType: "enterprise", FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour))}))
	for _, tc := range []struct {
		name         string
		organization pgtype.Text
		allowed      bool
	}{
		{name: "global", organization: pgtype.Text{}, allowed: false},
		{name: "same organization", organization: conv.ToPGText(fx.orgID), allowed: true},
		{name: "other organization", organization: conv.ToPGText(otherOrg), allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, err := testrepo.New(ti.conn).InsertOrganizationTierUserSessionIssuerFixture(ctx, testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{OrganizationID: tc.organization, Slug: uuid.NewString(), AuthnChallengeMode: "chain", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true}})
			require.NoError(t, err)
			issuer, err := usersessionsrepo.New(ti.conn).GetKitUserSessionIssuerByID(ctx, usersessionsrepo.GetKitUserSessionIssuerByIDParams{ID: id, ProjectID: fx.target.ProjectID, OrganizationID: fx.orgID})
			tx := testenv.BeginTx(t, ctx, ti.conn)
			_, attachmentErr := bindings.ValidateAndLock(ctx, tx, id, fx.target.ProjectID, fx.orgID)
			require.NoError(t, tx.Rollback(ctx))
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, id, issuer.ID)
				require.NoError(t, attachmentErr)
			} else {
				require.ErrorIs(t, attachmentErr, bindings.ErrNotFound)
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
		})
	}
}
