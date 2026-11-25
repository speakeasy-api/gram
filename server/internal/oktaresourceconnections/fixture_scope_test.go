package oktaresourceconnections_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
)

func TestSetIssuerURLFixture_RequiresOwnerScope(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	f := capableServer(t, ctx, si, "Scoped issuer")
	params := repo.SetIssuerURLFixtureParams{
		Issuer:         "https://updated.example",
		ID:             f.issuerID,
		OrganizationID: conv.ToPGText(si.orgID),
		ProjectID:      uuid.NullUUID{UUID: f.projectID, Valid: true},
	}

	wrongOrg := params
	wrongOrg.OrganizationID = conv.ToPGText(createOrganization(t, ctx, si.conn))
	n, err := si.q.SetIssuerURLFixture(ctx, wrongOrg)
	require.NoError(t, err)
	require.Zero(t, n)

	wrongProject := params
	wrongProject.ProjectID = uuid.NullUUID{UUID: createProject(t, ctx, si, si.orgID, "other-project"), Valid: true}
	n, err = si.q.SetIssuerURLFixture(ctx, wrongProject)
	require.NoError(t, err)
	require.Zero(t, n)

	n, err = si.q.SetIssuerURLFixture(ctx, params)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}
