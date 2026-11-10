package orgencryptionapi_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/org_encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestGetConfig_NotFoundBeforeFirstUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetConfig(asReader(t, ctx), &gen.GetConfigPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetConfig_ReportsTheManagedKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.provision(t)

	config, err := ti.service.GetConfig(asReader(t, ctx), &gen.GetConfigPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "speakeasy_managed", config.KeyMode)
	require.Equal(t, "active", config.ConfigState)

	keys, err := ti.service.ListKeys(asReader(t, ctx), &gen.ListKeysPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, keys.Keys, 1)
	require.True(t, keys.Keys[0].Managed)
	require.Nil(t, keys.Keys[0].ExternalCredentialID, "the platform credential is not exposed")
	require.Equal(t, config.KeyID, keys.Keys[0].ID)
}

func TestCreateGcpKey_RejectsAVersionedResourceName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpKey(asAdmin(t, ctx), &gen.CreateGcpKeyPayload{
		SessionToken:           nil,
		ExternalCredentialID:   ti.createCredential(t),
		Name:                   "versioned",
		ResourceName:           "projects/p/locations/global/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateGcpKey_RequiresAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpKey(asReader(t, ctx), &gen.CreateGcpKeyPayload{
		SessionToken:           nil,
		ExternalCredentialID:   ti.createCredential(t),
		Name:                   "key",
		ResourceName:           "projects/p/locations/global/keyRings/r/cryptoKeys/k",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_RequiresTheEntitlement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.ListKeys(asAdmin(t, ctx), &gen.ListKeysPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestVerifyKey_PassesForAUsableKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	key := ti.createKey(t, ctx, "customer-kek")

	result, err := ti.service.VerifyKey(asAdmin(t, ctx), &gen.VerifyKeyPayload{ID: key.ID, SessionToken: nil})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, "verified", result.KeyProbeOutcome)
}

func TestVerifyKey_ReportsADisabledKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	key := ti.createKey(t, ctx, "disabled-kek")
	require.NoError(t, ti.fixture.KMS.DisableEncryptionKey(t.Context(), key.ResourceName))

	result, err := ti.service.VerifyKey(asAdmin(t, ctx), &gen.VerifyKeyPayload{ID: key.ID, SessionToken: nil})
	require.NoError(t, err)
	require.False(t, result.Verified)
	require.Equal(t, "key_unusable", result.KeyProbeOutcome)
}

func TestSwitchKey_ToACustomerKeyThenComplete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.provision(t)
	key := ti.createKey(t, ctx, "customer-kek")

	switched, err := ti.service.SwitchKey(asAdmin(t, ctx), &gen.SwitchKeyPayload{ExternalKeyID: key.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "switching", switched.ConfigState)
	require.Equal(t, "customer_managed", switched.KeyMode)
	require.Equal(t, key.ID, switched.KeyID)
	require.NotNil(t, switched.PreviousKeyID)

	err = ti.service.DeleteKey(asAdmin(t, ctx), &gen.DeleteKeyPayload{ID: key.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	completed, err := ti.service.CompleteSwitch(asAdmin(t, ctx), &gen.CompleteSwitchPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "active", completed.ConfigState)
	require.Nil(t, completed.PreviousKeyID)

	err = ti.service.DeleteKey(asAdmin(t, ctx), &gen.DeleteKeyPayload{ID: *switched.PreviousKeyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSwitchKey_RollbackReturnsToTheManagedKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.provision(t)
	key := ti.createKey(t, ctx, "customer-kek")

	switched, err := ti.service.SwitchKey(asAdmin(t, ctx), &gen.SwitchKeyPayload{ExternalKeyID: key.ID, SessionToken: nil})
	require.NoError(t, err)

	rolledBack, err := ti.service.RollbackSwitch(asAdmin(t, ctx), &gen.RollbackSwitchPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, *switched.PreviousKeyID, rolledBack.KeyID)
	require.Equal(t, "speakeasy_managed", rolledBack.KeyMode)

	require.NoError(t, ti.service.DeleteKey(asAdmin(t, ctx), &gen.DeleteKeyPayload{ID: key.ID, SessionToken: nil}), "an unused customer key can be deleted")
}

func TestCompleteSwitch_ConflictsWithoutASwitch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.provision(t)

	_, err := ti.service.CompleteSwitch(asAdmin(t, ctx), &gen.CompleteSwitchPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestRotateDataKey_RejectsAnUnknownUseCase(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.provision(t)

	_, err := ti.service.RotateDataKey(asAdmin(t, ctx), &gen.RotateDataKeyPayload{UseCase: "not-registered", SessionToken: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.RotateDataKey(asAdmin(t, ctx), &gen.RotateDataKeyPayload{UseCase: "tailscale-gateway-state", SessionToken: nil})
	require.NoError(t, err)
}
