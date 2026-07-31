package externalkeys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestExternalKeys_CrossOrgIsolation verifies that a fully-granted admin in a
// different organization cannot read, update, or delete another org's key. It
// guards the organization_id predicate on every query.
func TestExternalKeys_CrossOrgIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Create a key in the default (mock) org.
	credID := createAwsIamCredential(t, ctx, ti, "org-a-cred")
	created := createAwsKmsKey(t, ctx, ti, "org-a-key", credID)

	// Build a context for a different organization, with full grants.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherOrg := *authCtx
	otherOrg.ActiveOrganizationID = "org_other_" + uuid.NewString()
	otherCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &otherOrg), authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))

	// The other org cannot read it.
	_, err := ti.service.GetAwsKmsKey(otherCtx, &gen.GetAwsKmsKeyPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	// The other org cannot update it.
	_, err = ti.service.UpdateAwsKmsKey(otherCtx, &gen.UpdateAwsKmsKeyPayload{
		ID:                     created.ID,
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:999999999999:key/attacker",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "hijacked",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	// The other org's delete is a no-op (does not touch org A's row).
	err = ti.service.DeleteAwsKmsKey(otherCtx, &gen.DeleteAwsKmsKeyPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	// The other org's list does not include it.
	otherList, err := ti.service.ListExternalKeys(otherCtx, &gen.ListExternalKeysPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotContains(t, keyIDs(otherList), created.ID)

	// The key is untouched for the owning org.
	got, err := ti.service.GetAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsKmsKeyPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "org-a-key", got.Name)
}

// TestExternalKeys_CrossOrgCredential verifies a key cannot be backed by a
// credential owned by a different organization.
func TestExternalKeys_CrossOrgCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Credential created in the default org.
	credID := createAwsIamCredential(t, ctx, ti, "org-a-cred")

	// A different org, fully granted, tries to back a key with org A's credential.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherOrg := *authCtx
	otherOrg.ActiveOrganizationID = "org_other_" + uuid.NewString()
	otherCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &otherOrg), authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))

	_, err := ti.service.CreateAwsKmsKey(otherCtx, &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "cross-org-cred",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
