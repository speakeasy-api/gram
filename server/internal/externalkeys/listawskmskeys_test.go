package externalkeys_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
)

func TestListAwsKmsKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")
	awsKey := createAwsKmsKey(t, ctx, ti, "aws-key", awsCredID)
	gcpKey := createGcpKmsKey(t, ctx, ti, "gcp-key", gcpCredID)

	result, err := ti.service.ListAwsKmsKeys(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListAwsKmsKeysPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)

	ids := keyIDs(result)
	require.Contains(t, ids, awsKey.ID)
	require.NotContains(t, ids, gcpKey.ID)
	require.Len(t, ids, 1)
}
