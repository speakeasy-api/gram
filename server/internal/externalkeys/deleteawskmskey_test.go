package externalkeys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteAwsKmsKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")
	key := createAwsKmsKey(t, ctx, ti, "doomed", credID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyDelete)
	require.NoError(t, err)

	err = ti.service.DeleteAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The key is gone.
	_, err = ti.service.GetAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestDeleteAwsKmsKey_MissingIsNoOp verifies deleting an unknown id is a
// no-op success (idempotent) and records no audit event.
func TestDeleteAwsKmsKey_MissingIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyDelete)
	require.NoError(t, err)

	err = ti.service.DeleteAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyDelete)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// TestDeleteAwsKmsKey_WrongProviderIsNoOp verifies deleting a gcp_kms key
// through the aws_kms endpoint does not delete it.
func TestDeleteAwsKmsKey_WrongProviderIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")
	gcpKey := createGcpKmsKey(t, ctx, ti, "gcp-key", gcpCredID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyDelete)
	require.NoError(t, err)

	err = ti.service.DeleteAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsKmsKeyPayload{
		ID:           gcpKey.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	// No aws_kms delete event is recorded for a cross-provider no-op.
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyDelete)
	require.NoError(t, err)
	require.Equal(t, before, after)

	// The gcp key is untouched.
	got, err := ti.service.GetGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpKmsKeyPayload{
		ID:           gcpKey.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, gcpKey.ID, got.ID)
}

func TestDeleteAwsKmsKey_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")
	key := createAwsKmsKey(t, ctx, ti, "key", credID)

	err := ti.service.DeleteAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.DeleteAwsKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
