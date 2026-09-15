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

// Nothing in the database stops this: external_credentials.deleted is a
// generated column, so the soft delete is an UPDATE and the external_keys
// foreign key never fires. Without the guard the delete would succeed and leave
// every key behind it pointing at a tombstone.
func TestDeleteGcpIamCredential_RefusedWhileKeyReferencesIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-delete-referenced")
	createGcpKmsKeyDirect(t, ctx, ti, created.ID, "dependent-key")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpIamCredentialDelete)
	require.NoError(t, err)

	err = ti.service.DeleteGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteGcpIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	// The refusal rolls back whole: no tombstone, and no audit event claiming one.
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpIamCredentialDelete)
	require.NoError(t, err)
	require.Equal(t, before, after)

	got, err := ti.service.GetGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
}

// The guard is shared by both providers but each takes its own audit branch, so
// the AWS arm is covered too. An aws_kms key can only be backed by an aws_iam
// credential, which is what makes this a distinct pairing rather than a repeat.
func TestDeleteAwsIamCredential_RefusedWhileKeyReferencesIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-delete-referenced")
	createAwsKmsKeyDirect(t, ctx, ti, created.ID, "dependent-aws-key")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialDelete)
	require.NoError(t, err)

	err = ti.service.DeleteAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialDelete)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// A soft-deleted key signs nothing, so it cannot be broken by removing the
// credential that reached it.
func TestDeleteGcpIamCredential_AllowedOnceKeyIsDeleted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-delete-freed")
	keyID := createGcpKmsKeyDirect(t, ctx, ti, created.ID, "short-lived-key")
	softDeleteGcpKmsKeyDirect(t, ctx, ti, keyID)

	err := ti.service.DeleteGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteGcpIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
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
