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

func TestDeleteGcpKmsKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "doomed", credID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyDelete)
	require.NoError(t, err)

	err = ti.service.DeleteGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	_, err = ti.service.GetGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestDeleteGcpKmsKey_MissingIsNoOp verifies deleting an unknown id is a no-op
// success (idempotent) and records no audit event.
func TestDeleteGcpKmsKey_MissingIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyDelete)
	require.NoError(t, err)

	err = ti.service.DeleteGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteGcpKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyDelete)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// TestDeleteGcpKmsKey_WrongProviderIsNoOp verifies deleting an aws_kms key
// through the gcp_kms endpoint does not delete it and records no audit event.
func TestDeleteGcpKmsKey_WrongProviderIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	awsKey := createAwsKmsKey(t, ctx, ti, "aws-key", awsCredID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyDelete)
	require.NoError(t, err)

	err = ti.service.DeleteGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteGcpKmsKeyPayload{
		ID:           awsKey.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	// No gcp_kms delete event is recorded for a cross-provider no-op.
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyDelete)
	require.NoError(t, err)
	require.Equal(t, before, after)

	// The aws key is untouched.
	got, err := ti.service.GetAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsKmsKeyPayload{
		ID:           awsKey.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, awsKey.ID, got.ID)
}

func TestDeleteGcpKmsKey_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "key", credID)

	err := ti.service.DeleteGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.DeleteGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
