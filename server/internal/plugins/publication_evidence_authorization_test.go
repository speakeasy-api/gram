package plugins_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestPublicationEvidenceRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.ResolvePublicationEvidence(context.Background(), ac.ActiveOrganizationID, *ac.ProjectID, nil)
	require.Error(t, err, "a caller without prepared grants must not read package addresses")
	_, err = ti.service.ResolvePublicationEvidence(authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID)}), ac.ActiveOrganizationID, *ac.ProjectID, nil)
	require.Error(t, err, "organization readers must not read admin package evidence")
	foreignOrgID := "org_evidence_" + uuid.NewString()
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 foreignOrgID,
		Name:               "Other organization",
		Slug:               "evidence-" + uuid.NewString()[:8],
		GramAccountType:    "free",
		FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
		CreatedAt:          pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}))
	foreignProjectID, err := fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
		ID: uuid.New(), Name: "Other project", Slug: "other-project", OrganizationID: foreignOrgID,
	})
	require.NoError(t, err)
	_, err = ti.service.ResolvePublicationEvidence(ctx, ac.ActiveOrganizationID, foreignProjectID, nil)
	require.ErrorContains(t, err, "does not belong to organization", "admin access does not bypass project ownership")
}
