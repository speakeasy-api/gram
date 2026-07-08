package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestExternalCredentials_CrossOrgIsolation verifies that a fully-granted admin
// in a different organization cannot read, update, or delete another org's
// credential. It guards the organization_id predicate on every query.
func TestExternalCredentials_CrossOrgIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Create a credential in the default (mock) org.
	created := createAWSExternalIDCredential(t, ctx, ti, "org-a-cred")

	// Build a context for a different organization, with full grants.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherOrg := *authCtx
	otherOrg.ActiveOrganizationID = "org_other_" + uuid.NewString()
	otherCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &otherOrg), authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))

	// The other org cannot read it.
	_, err := ti.service.GetAwsIamCredential(otherCtx, &gen.GetAwsIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	// The other org cannot update it.
	_, err = ti.service.UpdateAwsIamCredential(otherCtx, &gen.UpdateAwsIamCredentialPayload{
		ID:            created.ID,
		SessionToken:  nil,
		Name:          "hijacked",
		AssumeRoleArn: new("arn:aws:iam::999999999999:role/attacker"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	// The other org's delete is a no-op (does not touch org A's row).
	err = ti.service.DeleteAwsIamCredential(otherCtx, &gen.DeleteAwsIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	// The other org's list does not include it.
	otherList, err := ti.service.ListExternalCredentials(otherCtx, &gen.ListExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotContains(t, credentialIDs(otherList), created.ID)

	// The credential is untouched for the owning org.
	got, err := ti.service.GetAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "org-a-cred", got.Name)
}
