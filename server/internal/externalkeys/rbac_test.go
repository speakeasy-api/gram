package externalkeys_test

import (
	"testing"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestExternalKeys_ListForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ListExternalKeys(authztest.WithExactGrants(t, ctx), &gen.ListExternalKeysPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestExternalKeys_GetForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetGcpKmsKey(authztest.WithExactGrants(t, ctx), &gen.GetGcpKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestExternalKeys_CreateForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpKmsKey(authztest.WithExactGrants(t, ctx), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/1",
		ExternalCredentialID:   uuid.NewString(),
		Algorithm:              "ES256",
		Name:                   "denied",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestExternalKeys_VerifyForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.VerifyGcpKmsKey(authztest.WithExactGrants(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Verify reads nothing the org:read tier cannot already see, but it spends the
// key owner's money and writes to their audit log, so it sits with the writes.
func TestExternalKeys_VerifyForbiddenWithOnlyReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	readOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource))
	_, err := ti.service.VerifyGcpKmsKey(readOnly, &gen.VerifyGcpKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestExternalKeys_DeleteForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	err := ti.service.DeleteAwsKmsKey(authztest.WithExactGrants(t, ctx), &gen.DeleteAwsKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
