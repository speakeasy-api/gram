package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteAwsIamCredential_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-delete")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialDelete)
	require.NoError(t, err)

	err = ti.service.DeleteAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	_, err = ti.service.GetAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Deleting via the wrong-provider endpoint is a no-op: the credential survives
// and no audit event is recorded.
func TestDeleteAwsIamCredential_WrongProviderIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	gcp := createGCPImpersonationCredential(t, ctx, ti, "gcp-wrong-delete")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialDelete)
	require.NoError(t, err)

	err = ti.service.DeleteAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsIamCredentialPayload{
		ID:           gcp.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialDelete)
	require.NoError(t, err)
	require.Equal(t, before, after, "deleting a gcp credential via the aws endpoint records no aws delete event")

	// The GCP credential is untouched.
	_, err = ti.service.GetGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpIamCredentialPayload{
		ID:           gcp.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
}

func TestDeleteAwsIamCredential_Idempotent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialDelete)
	require.NoError(t, err)

	err = ti.service.DeleteAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsIamCredentialPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialDelete)
	require.NoError(t, err)
	require.Equal(t, before, after, "deleting a non-existent credential records no audit event")
}

func TestDeleteAwsIamCredential_InvalidID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	err := ti.service.DeleteAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsIamCredentialPayload{
		ID:           "not-a-uuid",
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestDeleteAwsIamCredential_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-delete-forbidden")

	err := ti.service.DeleteAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.DeleteAwsIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
