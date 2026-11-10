package gcpkms

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testEncryptionKeyName = testKeyRing + "/cryptoKeys/orgkek-1"

func TestLocalEncryptionClient_RoundTripsWithAssociatedData(t *testing.T) {
	t.Parallel()

	client, err := NewLocalEncryptionClient()
	require.NoError(t, err)

	encrypted, err := client.Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), []byte("aad"))
	require.NoError(t, err)
	require.Equal(t, testEncryptionKeyName+"/cryptoKeyVersions/1", encrypted.KeyVersionName)

	plaintext, err := client.Decrypt(t.Context(), testEncryptionKeyName, encrypted.Ciphertext, []byte("aad"))
	require.NoError(t, err)
	require.Equal(t, "dek", string(plaintext))
}

func TestLocalEncryptionClient_RefusesWrongAssociatedDataOrKey(t *testing.T) {
	t.Parallel()

	client, err := NewLocalEncryptionClient()
	require.NoError(t, err)

	encrypted, err := client.Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), []byte("aad"))
	require.NoError(t, err)

	_, err = client.Decrypt(t.Context(), testEncryptionKeyName, encrypted.Ciphertext, []byte("other"))
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = client.Decrypt(t.Context(), testKeyRing+"/cryptoKeys/orgkek-2", encrypted.Ciphertext, []byte("aad"))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestLocalEncryptionClient_DisabledKeyRefusesBothDirections(t *testing.T) {
	t.Parallel()

	client, err := NewLocalEncryptionClient()
	require.NoError(t, err)

	encrypted, err := client.Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), nil)
	require.NoError(t, err)

	require.NoError(t, client.DisableEncryptionKey(t.Context(), testEncryptionKeyName))

	_, err = client.Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), nil)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	_, err = client.Decrypt(t.Context(), testEncryptionKeyName, encrypted.Ciphertext, nil)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, ReasonKeyUnusable, ReasonForError(err))
}

func TestLocalEncryptionClient_RejectsVersionNames(t *testing.T) {
	t.Parallel()

	client, err := NewLocalEncryptionClient()
	require.NoError(t, err)

	_, err = client.Encrypt(t.Context(), testEncryptionKeyName+"/cryptoKeyVersions/1", []byte("dek"), nil)
	require.ErrorIs(t, err, ErrInvalidResourceName)
}

func TestLocalEncryptionClient_CreateEncryptionKeyNamesItsKey(t *testing.T) {
	t.Parallel()

	client, err := NewLocalEncryptionClient()
	require.NoError(t, err)

	created, err := client.CreateEncryptionKey(t.Context(), CreateEncryptionKeyParams{KeyRing: testKeyRing, KeyID: "orgkek-1"})
	require.NoError(t, err)
	require.Equal(t, testEncryptionKeyName, created.KeyName)

	_, err = client.CreateEncryptionKey(t.Context(), CreateEncryptionKeyParams{KeyRing: "not-a-ring", KeyID: "orgkek-1"})
	require.ErrorIs(t, err, ErrInvalidKeyRingName)
}

func TestPersistentLocalEncryptionClient_KeepsCiphertextReadableAcrossLoads(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "local-kms", "encryption.pem")

	first, err := NewPersistentLocalEncryptionClient(path)
	require.NoError(t, err)
	encrypted, err := first.Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), nil)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	second, err := NewPersistentLocalEncryptionClient(path)
	require.NoError(t, err)
	plaintext, err := second.Decrypt(t.Context(), testEncryptionKeyName, encrypted.Ciphertext, nil)
	require.NoError(t, err)
	require.Equal(t, "dek", string(plaintext))
}

func TestPersistentLocalEncryptionClient_RefusesGroupReadableFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "encryption.pem")
	_, err := NewPersistentLocalEncryptionClient(path)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0o640))

	_, err = NewPersistentLocalEncryptionClient(path)
	require.ErrorContains(t, err, "accessible only to its owner")
}

func TestVerifyEncryptionKey_PassesForAUsableKey(t *testing.T) {
	t.Parallel()

	client, err := NewLocalEncryptionClient()
	require.NoError(t, err)

	result := VerifyEncryptionKey(t.Context(), client, testEncryptionKeyName)
	require.True(t, result.Verified, result.Detail)
	require.Equal(t, ReasonVerified, result.Reason)
}

func TestVerifyEncryptionKey_ReportsDisabledKeyAsUnusable(t *testing.T) {
	t.Parallel()

	client, err := NewLocalEncryptionClient()
	require.NoError(t, err)
	require.NoError(t, client.DisableEncryptionKey(t.Context(), testEncryptionKeyName))

	result := VerifyEncryptionKey(t.Context(), client, testEncryptionKeyName)
	require.False(t, result.Verified)
	require.Equal(t, ReasonKeyUnusable, result.Reason)
}

func TestVerifyEncryptionKey_ReportsInvalidKeyName(t *testing.T) {
	t.Parallel()

	client, err := NewLocalEncryptionClient()
	require.NoError(t, err)

	result := VerifyEncryptionKey(t.Context(), client, testKeyRing)
	require.False(t, result.Verified)
	require.Equal(t, ReasonInvalidResourceName, result.Reason)
}
