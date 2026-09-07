package jsonwebkeysets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

// TestJsonWebKeySets_CrossOrgIsolation verifies that a fully-granted,
// fully-entitled admin in a different organization cannot read, mutate, or
// lifecycle another org's sets and keys. It guards the organization_id
// predicate on every query.
func TestJsonWebKeySets_CrossOrgIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "org-a-key")
	set := createSet(t, ctx, ti, "org-a-set", ek.ID)
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherOrg := *authCtx
	otherOrg.ActiveOrganizationID = "org_other_" + uuid.NewString()
	otherCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &otherOrg), authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))

	// Entitle the other org too, so what follows tests the organization_id
	// predicate rather than the entitlement gate short-circuiting ahead of it.
	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, otherOrg.ActiveOrganizationID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.GetSet(otherCtx, &gen.GetSetPayload{ID: set.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.UpdateSet(otherCtx, &gen.UpdateSetPayload{
		ID:            set.ID,
		SessionToken:  nil,
		Name:          "hijacked",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.ListKeys(otherCtx, &gen.ListKeysPayload{
		SetID:          set.ID,
		IncludeRevoked: true,
		SessionToken:   nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.PublishKey(otherCtx, &gen.PublishKeyPayload{SetID: set.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.ActivateKey(otherCtx, &gen.ActivateKeyPayload{ID: keys[0].ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.RetireKey(otherCtx, &gen.RetireKeyPayload{ID: keys[0].ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.RevokeKey(otherCtx, &gen.RevokeKeyPayload{ID: keys[0].ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	// The other org's create cannot be backed by org A's external key.
	_, err = ti.service.CreateSet(otherCtx, &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "cross-org",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// The other org's delete is a no-op (does not touch org A's row).
	require.NoError(t, ti.service.DeleteSet(otherCtx, &gen.DeleteSetPayload{ID: set.ID, SessionToken: nil}))

	// Everything is untouched for the owning org.
	got, err := ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{ID: set.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "org-a-set", got.Name)
	require.Len(t, listKeys(t, ctx, ti, set.ID, false), 1)
}
