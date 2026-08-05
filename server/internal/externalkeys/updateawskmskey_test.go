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

func TestUpdateAwsKmsKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")
	key := createAwsKmsKey(t, ctx, ti, "before", credID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   credID,
		Name:                   "after",
		CustomerGrantReference: new("arn:aws:iam::210987654321:role/gram-signer"),
	})
	require.NoError(t, err)

	require.Equal(t, key.ID, updated.ID)
	require.Equal(t, "after", updated.Name)
	require.NotNil(t, updated.CustomerGrantReference)
	require.Equal(t, "arn:aws:iam::210987654321:role/gram-signer", *updated.CustomerGrantReference)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// TestUpdateAwsKmsKey_IdentityIsImmutable verifies an update cannot re-point the
// row at different key material: the ARN and algorithm are set at creation and
// are not part of the update payload, so they survive an update untouched. A
// published JWK pins its kid to this row, so re-pointing it would silently sign
// with the wrong key.
func TestUpdateAwsKmsKey_IdentityIsImmutable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")
	key := createAwsKmsKey(t, ctx, ti, "before", credID)

	updated, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   credID,
		Name:                   "after",
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)

	require.Equal(t, key.KeyArn, updated.KeyArn)
	require.Equal(t, key.Algorithm, updated.Algorithm)

	// Re-read so the assertion covers what was persisted, not just the view the
	// update handler assembled.
	got, err := ti.service.GetAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, key.KeyArn, got.KeyArn)
	require.Equal(t, key.Algorithm, got.Algorithm)
	require.Equal(t, "after", got.Name)
}

// TestUpdateAwsKmsKey_ClearsOmittedGrantReference pins the replace-not-patch
// semantics of the mutable subset: omitting the optional customer_grant_reference
// clears it. Keeping omission meaningful is what makes clearing possible at all,
// since an absent field and an explicit null are indistinguishable once decoded.
func TestUpdateAwsKmsKey_ClearsOmittedGrantReference(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")

	key, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/" + uuid.NewString(),
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "granted",
		CustomerGrantReference: new("arn:aws:iam::210987654321:role/gram-signer"),
	})
	require.NoError(t, err)
	require.NotNil(t, key.CustomerGrantReference)

	updated, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   credID,
		Name:                   "granted",
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)
	require.Nil(t, updated.CustomerGrantReference)

	got, err := ti.service.GetAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Nil(t, got.CustomerGrantReference)
}

// TestUpdateAwsKmsKey_SwapCredential verifies the backing credential can be
// replaced with another same-family credential. The path to the key stays
// editable even though the key material is frozen.
func TestUpdateAwsKmsKey_SwapCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "cred-a")
	key := createAwsKmsKey(t, ctx, ti, "key", credID)
	otherCredID := createAwsIamCredential(t, ctx, ti, "cred-b")

	updated, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   otherCredID,
		Name:                   "key",
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)
	require.Equal(t, otherCredID, updated.ExternalCredentialID)
}

// TestUpdateAwsKmsKey_WrongFamilyCredential verifies the update rejects a swap
// to a gcp_iam credential.
func TestUpdateAwsKmsKey_WrongFamilyCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	key := createAwsKmsKey(t, ctx, ti, "key", credID)
	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")

	_, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   gcpCredID,
		Name:                   "key",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateAwsKmsKey_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")

	_, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     uuid.NewString(),
		SessionToken:           nil,
		ExternalCredentialID:   credID,
		Name:                   "missing",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestUpdateAwsKmsKey_WrongProvider verifies a gcp_kms key id cannot be updated
// through the aws_kms endpoint.
func TestUpdateAwsKmsKey_WrongProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")
	gcpKey := createGcpKmsKey(t, ctx, ti, "gcp-key", gcpCredID)

	_, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     gcpKey.ID,
		SessionToken:           nil,
		ExternalCredentialID:   awsCredID,
		Name:                   "hijack",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateAwsKmsKey_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")
	key := createAwsKmsKey(t, ctx, ti, "key", credID)

	_, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   credID,
		Name:                   "forbidden",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
