package openrouter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/cutover"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

// newMovedOrganization creates an organization moved to organization
// encryption and the sealer that moves it.
func newMovedOrganization(t *testing.T, conn *pgxpool.Pool) (string, *orgencryptiontest.Fixture, *cutover.Sealer) {
	t.Helper()

	orgID := "org-" + uuid.NewString()[:8]
	_, err := orgRepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Moved Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	fixture := orgencryptiontest.New(t, conn)
	flags := &feature.InMemory{}
	flags.SetFlag(cutover.AIProviderKeys.Flag, orgID, true)
	sealer := cutover.New(testenv.NewLogger(t), testenv.NewEncryptionClient(t), cutover.AIProviderKeys, &cutover.OrgEncryption{Service: fixture.Service, Provisioner: fixture.Provisioner, Flags: flags})

	return orgID, fixture, sealer
}

func requireOrgCopy(t *testing.T, fixture *orgencryptiontest.Fixture, row repo.OpenrouterApiKey, want string) {
	t.Helper()

	require.True(t, row.KeyOrgEncrypted.Valid, "the key has an organization-encrypted copy")
	encryptor, err := fixture.Service.For(row.OrganizationID, cutover.AIProviderKeys.UseCase)
	require.NoError(t, err)
	plaintext, err := encryptor.Decrypt(t.Context(), OrgEncryptedKeyColumn, row.KeyOrgEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, want, string(plaintext))

	legacy, err := testenv.NewEncryptionClient(t).Decrypt(row.KeyEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, want, legacy, "the application-key copy is kept")
}

func TestProvisionAPIKey_AddsTheOrganizationCopyAfterMinting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "orprovisionorgcopy")
	require.NoError(t, err)
	orgID, fixture, sealer := newMovedOrganization(t, conn)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/keys" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": 5.0, "hash": "hash-moved"},
			"key":  "sk-or-moved",
		})
	}))
	t.Cleanup(upstream.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, sealer)
	provisioner.baseURL = upstream.URL

	key, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "sk-or-moved", key)

	row, err := repo.New(conn).GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	requireOrgCopy(t, fixture, row, "sk-or-moved")

	key, err = provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "sk-or-moved", key, "later completions read the organization copy")
}

func TestLookupAPIKey_BackfillsAKeyWithoutAnOrganizationCopy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "orlookuporgcopy")
	require.NoError(t, err)
	orgID, fixture, sealer := newMovedOrganization(t, conn)

	ciphertext, err := testenv.NewEncryptionClient(t).Encrypt([]byte("sk-or-before-the-move"))
	require.NoError(t, err)
	_, err = repo.New(conn).CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
		KeyEncrypted:   pgtype.Text{String: ciphertext, Valid: true},
		KeyHash:        "hash-existing",
		MonthlyCredits: 5,
	})
	require.NoError(t, err)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, sealer)

	key, ok, err := provisioner.LookupAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "sk-or-before-the-move", key)

	row, err := repo.New(conn).GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeInternal)})
	require.NoError(t, err)
	require.Equal(t, ciphertext, row.KeyEncrypted.String)
	requireOrgCopy(t, fixture, row, "sk-or-before-the-move")
}
