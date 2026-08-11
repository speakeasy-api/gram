package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestDeleteGcpIamCredential_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-delete")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpIamCredentialDelete)
	require.NoError(t, err)

	err = ti.service.DeleteGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteGcpIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpIamCredentialDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestDeleteGcpIamCredential_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-delete-no-entitlement")
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	err := ti.service.DeleteGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteGcpIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
