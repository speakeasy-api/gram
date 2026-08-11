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

func TestCreateAwsKmsKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyCreate)
	require.NoError(t, err)

	key, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "signing-key",
		CustomerGrantReference: new("arn:aws:iam::210987654321:role/gram-signer"),
	})
	require.NoError(t, err)
	require.NotNil(t, key)

	require.Equal(t, "aws_kms", key.Provider)
	require.Equal(t, "RS256", key.Algorithm)
	require.Equal(t, "signing-key", key.Name)
	require.Equal(t, "arn:aws:kms:us-east-1:123456789012:key/abcd-1234", key.KeyArn)
	require.Equal(t, credID, key.ExternalCredentialID)
	require.NotNil(t, key.CustomerGrantReference)
	require.Equal(t, "arn:aws:iam::210987654321:role/gram-signer", *key.CustomerGrantReference)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsKmsKeyCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestCreateAwsKmsKey_WithoutGrantReference(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")

	key, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/no-grant",
		ExternalCredentialID:   credID,
		Algorithm:              "ES256",
		Name:                   "no-grant-key",
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)
	require.Nil(t, key.CustomerGrantReference)
}

func TestCreateAwsKmsKey_MissingName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")

	_, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "   ",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateAwsKmsKey_MissingKeyArn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")

	_, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "   ",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "missing-arn",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateAwsKmsKey_InvalidCredentialID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   "not-a-uuid",
		Algorithm:              "RS256",
		Name:                   "bad-cred-id",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateAwsKmsKey_CredentialNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   uuid.NewString(),
		Algorithm:              "RS256",
		Name:                   "missing-cred",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestCreateAwsKmsKey_WrongFamilyCredential verifies an aws_kms key cannot be
// backed by a gcp_iam credential.
func TestCreateAwsKmsKey_WrongFamilyCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")

	_, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   gcpCredID,
		Algorithm:              "RS256",
		Name:                   "wrong-family",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateAwsKmsKey_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")

	_, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "forbidden",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
