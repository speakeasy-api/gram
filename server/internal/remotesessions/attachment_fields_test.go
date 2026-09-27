package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Attachment metadata must survive generated repository scans, not just exist
// in the database: credential snapshots consume the returned generation.
func TestAttachmentFieldsSurviveRepositoryScans(t *testing.T) {
	t.Parallel()

	ctx, fixture := seedSharedGrantAcrossIssuers(t)
	require.Equal(t, int64(1), fixture.session.GrantGeneration)

	issuer, err := usersessionsrepo.New(fixture.ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:             fixture.issuerA,
		ProjectID:      fixture.projectID,
		OrganizationID: fixture.organizationID,
	})
	require.NoError(t, err)
	require.True(t, issuer.AttachmentScope.Valid)
	require.Equal(t, "project:"+fixture.projectID.String(), issuer.AttachmentScope.String)
}
