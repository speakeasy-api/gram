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
		KeyArn:                 "arn:aws:kms:eu-west-1:123456789012:key/rotated",
		ExternalCredentialID:   credID,
		Algorithm:              "ES256",
		Name:                   "after",
		CustomerGrantReference: new("arn:aws:iam::210987654321:role/gram-signer"),
	})
	require.NoError(t, err)

	require.Equal(t, key.ID, updated.ID)
	require.Equal(t, "after", updated.Name)
	require.Equal(t, "ES256", updated.Algorithm)
	require.Equal(t, "arn:aws:kms:eu-west-1:123456789012:key/rotated", updated.KeyArn)
	require.NotNil(t, updated.CustomerGrantReference)
	require.Equal(t, "arn:aws:iam::210987654321:role/gram-signer", *updated.CustomerGrantReference)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// TestUpdateAwsKmsKey_SwapCredential verifies the backing credential can be
// replaced with another same-family credential.
func TestUpdateAwsKmsKey_SwapCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "cred-a")
	key := createAwsKmsKey(t, ctx, ti, "key", credID)
	otherCredID := createAwsIamCredential(t, ctx, ti, "cred-b")

	updated, err := ti.service.UpdateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		KeyArn:                 key.KeyArn,
		ExternalCredentialID:   otherCredID,
		Algorithm:              "RS256",
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
		KeyArn:                 key.KeyArn,
		ExternalCredentialID:   gcpCredID,
		Algorithm:              "RS256",
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
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
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
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   awsCredID,
		Algorithm:              "RS256",
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
		KeyArn:                 key.KeyArn,
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "forbidden",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
