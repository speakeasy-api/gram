package workloadidentity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

func TestResolveIssuerByURL_ForbiddenKindShadowsRemoteOrganizationIssuer(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"system", "unknown", ""} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			conn, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			tenant := newTenant(t, conn)
			orgID := seedIssuer(t, conn, tenant.organizationID, organizationTier(), "remote organization", testIssuerURL, epoch)
			projectID := seedIssuer(t, conn, tenant.organizationID, projectTier(tenant.projectID), "project override", testIssuerURL, epoch)
			_, err = conn.Exec(t.Context(), `UPDATE workload_issuers SET issuer_kind = $2, jwks_uri = CASE WHEN $2 = 'system' THEN '' ELSE jwks_uri END, allow_wildcard_admission = false WHERE id = $1`, projectID, kind) //nolint:glint // notestingrawsql: fixture needs unsupported issuer kinds not writable through the public API
			require.NoError(t, err)
			resolved, err := workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
				OrganizationID: tenant.organizationID, ProjectID: projectTier(tenant.projectID), IssuerURL: testIssuerURL,
			})
			require.ErrorIs(t, err, workloadidentity.ErrIssuerNotFound)
			require.Zero(t, resolved, "a forbidden selected issuer must not fall back to the remote organization issuer")
			resolved, err = workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
				OrganizationID: tenant.organizationID, ProjectID: organizationTier(), IssuerURL: testIssuerURL,
			})
			require.NoError(t, err)
			require.Equal(t, orgID, resolved.ID, "the remote issuer remains usable outside the forbidden project's scope")
		})
	}
}
