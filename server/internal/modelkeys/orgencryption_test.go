package modelkeys_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	modelkeysrepo "github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/cutover"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const orgEncryptedKeyColumn = "model_provider_keys.api_key_org_encrypted"

func openOrgCopy(t *testing.T, fixture *orgencryptiontest.Fixture, orgID string, ciphertext string) string {
	t.Helper()

	encryptor, err := fixture.Service.For(orgID, cutover.AIProviderKeys.UseCase)
	require.NoError(t, err)

	plaintext, err := encryptor.Decrypt(t.Context(), orgEncryptedKeyColumn, ciphertext)
	require.NoError(t, err)

	return string(plaintext)
}

func TestUpsertKey_SealsBothCopiesForAMovedOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti, fixture := newOrgEncryptedTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	upsertTestKey(t, ctx, ti, modelkeys.SlotDefault, "sk-or-moved")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := modelkeysrepo.New(ti.conn).ListKeysByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	legacy, err := ti.enc.Decrypt(rows[0].ApiKeyEncrypted)
	require.NoError(t, err)
	require.Equal(t, "sk-or-moved", legacy)
	require.True(t, rows[0].ApiKeyOrgEncrypted.Valid)
	require.Equal(t, "sk-or-moved", openOrgCopy(t, fixture, orgID, rows[0].ApiKeyOrgEncrypted.String))

	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "sk-or-moved", resolved.Key)
	require.True(t, resolved.Customer)
}

func TestResolveKey_BackfillsAKeyWithoutAnOrganizationCopy(t *testing.T) {
	t.Parallel()

	ctx, ti, fixture := newOrgEncryptedTestService(t)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	legacy, err := ti.enc.Encrypt([]byte("sk-or-written-before-the-move"))
	require.NoError(t, err)
	inserted, err := modelkeysrepo.New(ti.conn).InsertKey(ctx, modelkeysrepo.InsertKeyParams{
		OrganizationID:     orgID,
		ProjectID:          *authCtx.ProjectID,
		Slot:               modelkeys.SlotDefault,
		Provider:           modelkeys.ProviderOpenRouter,
		ApiKeyEncrypted:    legacy,
		ApiKeyOrgEncrypted: pgtype.Text{String: "", Valid: false},
		Enabled:            true,
	})
	require.NoError(t, err)

	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "sk-or-written-before-the-move", resolved.Key)

	row, err := modelkeysrepo.New(ti.conn).GetKeyByIDForUpdate(ctx, modelkeysrepo.GetKeyByIDForUpdateParams{ID: inserted.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, legacy, row.ApiKeyEncrypted, "the application-key copy is kept")
	require.True(t, row.ApiKeyOrgEncrypted.Valid, "the first read adds the organization copy")
	require.Equal(t, "sk-or-written-before-the-move", openOrgCopy(t, fixture, orgID, row.ApiKeyOrgEncrypted.String))
}

func TestResolveKey_FailsClosedWhileOrganizationEncryptionIsSuspended(t *testing.T) {
	t.Parallel()

	ctx, ti, fixture := newOrgEncryptedTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	upsertTestKey(t, ctx, ti, modelkeys.SlotDefault, "sk-or-moved")
	require.NoError(t, fixture.Provisioner.Suspend(ctx, orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
}
