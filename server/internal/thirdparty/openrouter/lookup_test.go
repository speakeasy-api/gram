package openrouter

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

// TestLookupAPIKey_NoRowDoesNotProvision pins the read-only contract: an
// organization with no key of the requested type reads as not provisioned,
// and no upstream key is minted along the way.
func TestLookupAPIKey_NoRowDoesNotProvision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "orlookupnorow")
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Lookup Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, nil, testenv.NewEncryptionClient(t))
	provisioner.baseURL = upstream.URL

	key, ok, err := provisioner.LookupAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, key)
	require.Zero(t, upstreamCalls.Load(), "a lookup must never create an upstream key")

	_, err = repo.New(conn).GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.Error(t, err, "a lookup must never insert a key row")
}

// TestLookupAPIKey_ExistingRow pins that a provisioned key resolves through
// the same decrypt path ProvisionAPIKey uses and that a disabled key is
// refused rather than reported as missing.
func TestLookupAPIKey_ExistingRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "orlookuprow")
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Lookup Row Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	ciphertext, err := enc.Encrypt([]byte("sk-or-existing"))
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
	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, nil, enc)

	key, ok, err := provisioner.LookupAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "sk-or-existing", key)

	require.NoError(t, repo.New(conn).DisableOpenRouterAPIKey(ctx, repo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	}))

	key, ok, err = provisioner.LookupAPIKey(ctx, orgID, KeyTypeInternal)
	require.ErrorIs(t, err, ErrPlatformKeyDisabled)
	require.False(t, ok)
	require.Empty(t, key)
}
