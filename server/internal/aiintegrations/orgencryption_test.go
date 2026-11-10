package aiintegrations

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/ai_integrations"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/cutover"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// moveToOrgEncryption switches the store to a sealer that has moved orgID to
// organization encryption.
func moveToOrgEncryption(t *testing.T, store *Store, orgID string) *orgencryptiontest.Fixture {
	t.Helper()

	fixture := orgencryptiontest.New(t, store.db)
	flags := &feature.InMemory{}
	flags.SetFlag(cutover.AIProviderKeys.Flag, orgID, true)
	store.keys = cutover.New(testenv.NewLogger(t), testenv.NewEncryptionClient(t), cutover.AIProviderKeys, &cutover.OrgEncryption{Service: fixture.Service, Provisioner: fixture.Provisioner, Flags: flags})

	return fixture
}

func openOrgCopy(t *testing.T, ctx context.Context, fixture *orgencryptiontest.Fixture, row repo.AiIntegrationConfig) string {
	t.Helper()

	require.True(t, row.ApiKeyOrgEncrypted.Valid, "the credential has an organization-encrypted copy")
	encryptor, err := fixture.Service.For(row.OrganizationID, cutover.AIProviderKeys.UseCase)
	require.NoError(t, err)
	plaintext, err := encryptor.Decrypt(ctx, orgEncryptedAPIKeyColumn, row.ApiKeyOrgEncrypted.String)
	require.NoError(t, err)

	return string(plaintext)
}

func TestUpsertWithTx_SealsBothCopiesForAMovedOrganization(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	fixture := moveToOrgEncryption(t, store, orgID)

	result := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, nil)
	require.Equal(t, "cursor-key", result.Config.APIKey)
	require.Equal(t, "cursor-key", openOrgCopy(t, ctx, fixture, *result.Row))

	legacy, err := testenv.NewEncryptionClient(t).Decrypt(result.Row.ApiKeyEncrypted)
	require.NoError(t, err)
	require.Equal(t, "cursor-key", legacy, "the application-key copy is kept")
}

func TestLoadConfig_BackfillsACredentialWithoutAnOrganizationCopy(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	written := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, nil)
	require.False(t, written.Row.ApiKeyOrgEncrypted.Valid)

	fixture := moveToOrgEncryption(t, store, orgID)
	cfg, _, err := store.loadForOrgAndProviderRow(ctx, orgID, ProviderCursor)
	require.NoError(t, err)
	require.Equal(t, "cursor-key", cfg.APIKey)

	rows, err := repo.New(conn).ListEnabledConfigsByProvider(ctx, ProviderCursor)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, written.Row.ApiKeyEncrypted, rows[0].ApiKeyEncrypted, "the application-key copy is kept")
	require.True(t, rows[0].ApiKeyOrgEncrypted.Valid, "the first read adds the organization copy")

	encryptor, err := fixture.Service.For(orgID, cutover.AIProviderKeys.UseCase)
	require.NoError(t, err)
	plaintext, err := encryptor.Decrypt(ctx, orgEncryptedAPIKeyColumn, rows[0].ApiKeyOrgEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "cursor-key", string(plaintext))
}

func TestAnthropicInferenceResolver_ReadsTheOrganizationCopyForAMovedOrganization(t *testing.T) {
	t.Parallel()

	ctx, conn, service, orgID := newInferenceTestService(t)
	fixture := moveToOrgEncryption(t, service.store, orgID)

	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte("EXAMPLE-inference-secret-moved"))
	enabled := true
	saved, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{SigningSecret: &secret, Enabled: &enabled})
	require.NoError(t, err)

	row, err := service.store.repo.GetAnthropicInferenceConfig(ctx, orgID)
	require.NoError(t, err)
	require.Contains(t, openOrgCopy(t, ctx, fixture, row), secret)

	resolver := NewAnthropicInferenceResolver(conn, service.store.keys)
	binding, err := resolver.Resolve(ctx, *saved.ID)
	require.NoError(t, err)
	require.Equal(t, []string{secret}, binding.SigningSecrets)

	require.NoError(t, fixture.Provisioner.Suspend(ctx, orgID, "customer disabled the key", orgencryption.SystemActor()))
	_, err = resolver.Resolve(ctx, *saved.ID)
	require.ErrorIs(t, err, orgencryption.ErrSuspended, "a moved organization never falls back to the application key")
}
