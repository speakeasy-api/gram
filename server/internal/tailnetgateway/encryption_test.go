package tailnetgateway_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
)

// The columns the gateway binds its ciphertexts to. Repeated here so a rename
// that would make stored values unreadable fails a test.
const (
	nodeStateColumn = "network_ingress_node_states.value_encrypted"
	authKeyColumn   = "network_ingress_nodes.auth_key_encrypted"
)

// orgEncryptionFixtures holds one organization encryption fixture per test
// database, so every helper and component in a test shares its keys.
var orgEncryptionFixtures sync.Map

func orgEncryption(t *testing.T, db *pgxpool.Pool) *orgencryptiontest.Fixture {
	t.Helper()
	if existing, ok := orgEncryptionFixtures.Load(db); ok {
		fixture, ok := existing.(*orgencryptiontest.Fixture)
		require.True(t, ok)
		return fixture
	}
	stored, _ := orgEncryptionFixtures.LoadOrStore(db, orgencryptiontest.New(t, db))
	fixture, ok := stored.(*orgencryptiontest.Fixture)
	require.True(t, ok)
	return fixture
}

// provisionOrg mints the organization's gateway data key, as the reconciler
// does before the gateway ever runs.
func provisionOrg(t *testing.T, db *pgxpool.Pool, orgID string) {
	t.Helper()
	require.NoError(t, orgEncryption(t, db).Provisioner.EnsureUseCase(t.Context(), orgID, tailnetgateway.EncryptionUseCase, orgencryption.SystemActor()))
}

func gatewayEncryptor(t *testing.T, db *pgxpool.Pool, orgID string) *orgencryption.Encryptor {
	t.Helper()
	provisionOrg(t, db, orgID)
	encryptor, err := orgEncryption(t, db).Service.For(orgID, tailnetgateway.EncryptionUseCase)
	require.NoError(t, err)
	return encryptor
}

func sealAuthKey(t *testing.T, db *pgxpool.Pool, orgID, authKey string) string {
	t.Helper()
	ciphertext, err := gatewayEncryptor(t, db, orgID).Encrypt(t.Context(), authKeyColumn, []byte(authKey))
	require.NoError(t, err)
	return ciphertext
}

func openAuthKey(t *testing.T, db *pgxpool.Pool, orgID, ciphertext string) string {
	t.Helper()
	plaintext, err := gatewayEncryptor(t, db, orgID).Decrypt(t.Context(), authKeyColumn, ciphertext)
	require.NoError(t, err)
	return string(plaintext)
}

func newStateStore(t *testing.T, db *pgxpool.Pool, ingressID uuid.UUID, orgID string, slot int32) *tailnetgateway.PostgresStateStore {
	t.Helper()
	provisionOrg(t, db, orgID)
	store, err := tailnetgateway.NewPostgresStateStore(db, orgEncryption(t, db).Service, ingressID, orgID, slot)
	require.NoError(t, err)
	return store
}

func suspendOrgEncryption(t *testing.T, db *pgxpool.Pool, orgID string) {
	t.Helper()
	require.NoError(t, orgEncryption(t, db).Provisioner.Suspend(t.Context(), orgID, "test", orgencryption.SystemActor()))
}
