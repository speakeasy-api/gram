package jsonwebkeysets_test

import (
	"testing"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestJsonWebKeySets_ListForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ListSets(authztest.WithExactGrants(t, ctx), &gen.ListSetsPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestJsonWebKeySets_CreateForbiddenWithOnlyReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateSet(readCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "denied",
		ExternalKeyID: uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestJsonWebKeySets_UpdateForbiddenWithOnlyReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateSet(readCtx(t, ctx), &gen.UpdateSetPayload{
		ID:            uuid.NewString(),
		SessionToken:  nil,
		Name:          "denied",
		ExternalKeyID: uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestJsonWebKeySets_DeleteForbiddenWithOnlyReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	err := ti.service.DeleteSet(readCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestJsonWebKeySets_PublishForbiddenWithOnlyReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.PublishKey(readCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestJsonWebKeySets_ActivateForbiddenWithOnlyReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ActivateKey(readCtx(t, ctx), &gen.ActivateKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestJsonWebKeySets_RetireForbiddenWithOnlyReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.RetireKey(readCtx(t, ctx), &gen.RetireKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestJsonWebKeySets_RevokeForbiddenWithOnlyReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.RevokeKey(readCtx(t, ctx), &gen.RevokeKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestJsonWebKeySets_ForbiddenWithoutEntitlement asserts the server-side
// customer_managed_encryption_keys gate: full grants are not enough for an
// organization the feature was never enabled for.
func TestJsonWebKeySets_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.ListSets(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.ListSetsPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.CreateSet(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "denied",
		ExternalKeyID: uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
