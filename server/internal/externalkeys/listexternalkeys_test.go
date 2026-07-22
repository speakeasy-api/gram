package externalkeys_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListExternalKeys_All(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")
	awsKey := createAwsKmsKey(t, ctx, ti, "aws-key", awsCredID)
	gcpKey := createGcpKmsKey(t, ctx, ti, "gcp-key", gcpCredID)

	result, err := ti.service.ListExternalKeys(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListExternalKeysPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)

	ids := keyIDs(result)
	require.Contains(t, ids, awsKey.ID)
	require.Contains(t, ids, gcpKey.ID)
	require.Len(t, ids, 2)
}

func TestListExternalKeys_ProviderFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")
	awsKey := createAwsKmsKey(t, ctx, ti, "aws-key", awsCredID)
	gcpKey := createGcpKmsKey(t, ctx, ti, "gcp-key", gcpCredID)

	result, err := ti.service.ListExternalKeys(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListExternalKeysPayload{
		Provider:     new("aws_kms"),
		SessionToken: nil,
	})
	require.NoError(t, err)

	ids := keyIDs(result)
	require.Contains(t, ids, awsKey.ID)
	require.NotContains(t, ids, gcpKey.ID)
}

func TestListExternalKeys_ForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ListExternalKeys(authztest.WithExactGrants(t, ctx), &gen.ListExternalKeysPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
