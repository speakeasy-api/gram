package gcpkms

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// fakeEncryptionRing extends fakeKeyRing with the symmetric surface. Its
// "ciphertext" is the associated data and plaintext joined with a marker, so
// tests can see exactly what was sent.
type fakeEncryptionRing struct {
	*fakeKeyRing

	// lastEncrypt and lastDecrypt record the most recent requests.
	lastEncrypt *kmspb.EncryptRequest
	lastDecrypt *kmspb.DecryptRequest

	// primaryVersion is the version Encrypt reports.
	primaryVersion string

	// encryptName overrides the version name Encrypt reports.
	encryptName string

	// skipPlaintextVerification answers Encrypt without verifying the
	// plaintext checksum.
	skipPlaintextVerification bool

	// corruptCiphertextChecksum answers Encrypt with a wrong ciphertext checksum.
	corruptCiphertextChecksum bool

	// corruptPlaintextChecksum answers Decrypt with a wrong plaintext checksum.
	corruptPlaintextChecksum bool

	// primaryState is the state CreateCryptoKey reports for the first version.
	primaryState kmspb.CryptoKeyVersion_CryptoKeyVersionState

	// versions lists the versions ListCryptoKeyVersions returns per key.
	versions map[string][]string
}

func newFakeEncryptionRing() *fakeEncryptionRing {
	return &fakeEncryptionRing{
		fakeKeyRing:    newFakeKeyRing(),
		primaryVersion: "3",
		primaryState:   kmspb.CryptoKeyVersion_ENABLED,
		versions:       map[string][]string{},
	}
}

func newFakeEncryptionClient(t *testing.T, ring *fakeEncryptionRing) *kmsEncryptionClient {
	t.Helper()

	return &kmsEncryptionClient{kms: newFakeProvisioningClient(t, ring, ring).kms}
}

func (f *fakeEncryptionRing) CreateCryptoKey(ctx context.Context, req *kmspb.CreateCryptoKeyRequest) (*kmspb.CryptoKey, error) {
	key, err := f.fakeKeyRing.CreateCryptoKey(ctx, req)
	if err != nil {
		return nil, err
	}

	key.Primary = &kmspb.CryptoKeyVersion{Name: key.GetName() + "/cryptoKeyVersions/1", State: f.primaryState}
	f.mu.Lock()
	f.versions[key.GetName()] = []string{key.GetName() + "/cryptoKeyVersions/1"}
	f.mu.Unlock()

	return key, nil
}

func (f *fakeEncryptionRing) GetCryptoKey(_ context.Context, req *kmspb.GetCryptoKeyRequest) (*kmspb.CryptoKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.keys[req.GetName()]; !ok {
		return nil, status.Error(codes.NotFound, "no such key") //nolint:wrapcheck // the fake speaks gRPC status codes, as GCP does
	}

	return &kmspb.CryptoKey{Name: req.GetName()}, nil
}

func (f *fakeEncryptionRing) ListCryptoKeyVersions(_ context.Context, req *kmspb.ListCryptoKeyVersionsRequest) (*kmspb.ListCryptoKeyVersionsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	resp := &kmspb.ListCryptoKeyVersionsResponse{}
	for _, name := range f.versions[req.GetParent()] {
		resp.CryptoKeyVersions = append(resp.CryptoKeyVersions, &kmspb.CryptoKeyVersion{Name: name, State: kmspb.CryptoKeyVersion_ENABLED})
	}

	return resp, nil
}

func (f *fakeEncryptionRing) Encrypt(_ context.Context, req *kmspb.EncryptRequest) (*kmspb.EncryptResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastEncrypt = req
	ciphertext := append(append(append([]byte(nil), req.GetAdditionalAuthenticatedData()...), '|'), req.GetPlaintext()...)
	checksum := crc32c(ciphertext)
	if f.corruptCiphertextChecksum {
		checksum++
	}
	name := req.GetName() + "/cryptoKeyVersions/" + f.primaryVersion
	if f.encryptName != "" {
		name = f.encryptName
	}

	return &kmspb.EncryptResponse{
		Name:                    name,
		Ciphertext:              ciphertext,
		CiphertextCrc32C:        wrapperspb.Int64(checksum),
		VerifiedPlaintextCrc32C: !f.skipPlaintextVerification && req.GetPlaintextCrc32C().GetValue() == crc32c(req.GetPlaintext()),
		VerifiedAdditionalAuthenticatedDataCrc32C: req.GetAdditionalAuthenticatedDataCrc32C() != nil &&
			req.GetAdditionalAuthenticatedDataCrc32C().GetValue() == crc32c(req.GetAdditionalAuthenticatedData()),
	}, nil
}

func (f *fakeEncryptionRing) Decrypt(_ context.Context, req *kmspb.DecryptRequest) (*kmspb.DecryptResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastDecrypt = req
	prefix := append(append([]byte(nil), req.GetAdditionalAuthenticatedData()...), '|')
	if !bytes.HasPrefix(req.GetCiphertext(), prefix) {
		return nil, status.Error(codes.InvalidArgument, "decryption failed") //nolint:wrapcheck // the fake speaks gRPC status codes, as GCP does
	}
	plaintext := req.GetCiphertext()[len(prefix):]
	checksum := crc32c(plaintext)
	if f.corruptPlaintextChecksum {
		checksum++
	}

	return &kmspb.DecryptResponse{Plaintext: plaintext, PlaintextCrc32C: wrapperspb.Int64(checksum)}, nil
}

func TestKMSEncrypt_SendsChecksumsAndReportsVersion(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	client := newFakeEncryptionClient(t, ring)

	result, err := client.Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), []byte("aad"))
	require.NoError(t, err)
	require.Equal(t, testEncryptionKeyName+"/cryptoKeyVersions/3", result.KeyVersionName)
	require.Equal(t, crc32c([]byte("dek")), ring.lastEncrypt.GetPlaintextCrc32C().GetValue())
	require.Equal(t, crc32c([]byte("aad")), ring.lastEncrypt.GetAdditionalAuthenticatedDataCrc32C().GetValue())

	plaintext, err := client.Decrypt(t.Context(), testEncryptionKeyName, result.Ciphertext, []byte("aad"))
	require.NoError(t, err)
	require.Equal(t, "dek", string(plaintext))
	require.Equal(t, crc32c(result.Ciphertext), ring.lastDecrypt.GetCiphertextCrc32C().GetValue())
}

func TestKMSEncrypt_OmitsAssociatedDataChecksumWhenEmpty(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	client := newFakeEncryptionClient(t, ring)

	_, err := client.Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), nil)
	require.NoError(t, err)
	require.Nil(t, ring.lastEncrypt.GetAdditionalAuthenticatedDataCrc32C())
}

func TestKMSEncrypt_RejectsUnverifiedPlaintextChecksum(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	ring.skipPlaintextVerification = true

	_, err := newFakeEncryptionClient(t, ring).Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), nil)
	require.ErrorIs(t, err, ErrEncryptionResponseInvalid)
	require.Equal(t, ReasonUnavailable, ReasonForError(err))
}

func TestKMSEncrypt_RejectsCiphertextChecksumMismatch(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	ring.corruptCiphertextChecksum = true

	_, err := newFakeEncryptionClient(t, ring).Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), nil)
	require.ErrorIs(t, err, ErrEncryptionResponseInvalid)
}

func TestKMSEncrypt_RejectsResponseForADifferentKey(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	ring.encryptName = testKeyRing + "/cryptoKeys/other/cryptoKeyVersions/1"

	_, err := newFakeEncryptionClient(t, ring).Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), nil)
	require.ErrorIs(t, err, ErrEncryptionResponseInvalid)
}

func TestKMSDecrypt_RejectsPlaintextChecksumMismatch(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	client := newFakeEncryptionClient(t, ring)
	result, err := client.Encrypt(t.Context(), testEncryptionKeyName, []byte("dek"), nil)
	require.NoError(t, err)

	ring.corruptPlaintextChecksum = true
	_, err = client.Decrypt(t.Context(), testEncryptionKeyName, result.Ciphertext, nil)
	require.ErrorIs(t, err, ErrEncryptionResponseInvalid)
}

func TestKMSDecrypt_PreservesGRPCStatus(t *testing.T) {
	t.Parallel()

	_, err := newFakeEncryptionClient(t, newFakeEncryptionRing()).Decrypt(t.Context(), testEncryptionKeyName, []byte("garbage"), []byte("aad"))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestKMSEncrypt_RejectsVersionNamesBeforeCalling(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	_, err := newFakeEncryptionClient(t, ring).Encrypt(t.Context(), testEncryptionKeyName+"/cryptoKeyVersions/1", []byte("dek"), nil)
	require.ErrorIs(t, err, ErrInvalidResourceName)
	require.Nil(t, ring.lastEncrypt)
}

func TestCreateEncryptionKey_CreatesSoftwareSymmetricKey(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	created, err := newFakeEncryptionClient(t, ring).CreateEncryptionKey(t.Context(), CreateEncryptionKeyParams{KeyRing: testKeyRing, KeyID: "orgkek-1"})
	require.NoError(t, err)
	require.Equal(t, testEncryptionKeyName, created.KeyName)

	req := ring.createdKey(t, testEncryptionKeyName)
	require.Equal(t, kmspb.CryptoKey_ENCRYPT_DECRYPT, req.GetCryptoKey().GetPurpose())
	require.Equal(t, kmspb.CryptoKeyVersion_GOOGLE_SYMMETRIC_ENCRYPTION, req.GetCryptoKey().GetVersionTemplate().GetAlgorithm())
	require.Equal(t, kmspb.ProtectionLevel_SOFTWARE, req.GetCryptoKey().GetVersionTemplate().GetProtectionLevel())
	require.Nil(t, req.GetCryptoKey().GetRotationSchedule())
}

func TestCreateEncryptionKey_DisablesKeyWhosePrimaryIsNotEnabled(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	ring.primaryState = kmspb.CryptoKeyVersion_DISABLED

	_, err := newFakeEncryptionClient(t, ring).CreateEncryptionKey(t.Context(), CreateEncryptionKeyParams{KeyRing: testKeyRing, KeyID: "orgkek-1"})
	require.ErrorContains(t, err, "expected ENABLED")
	require.Equal(t, []string{testEncryptionKeyName + "/cryptoKeyVersions/1"}, ring.disabledVersions())
}

func TestCreateEncryptionKey_ReportsExistingKey(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	client := newFakeEncryptionClient(t, ring)
	params := CreateEncryptionKeyParams{KeyRing: testKeyRing, KeyID: "orgkek-1"}

	_, err := client.CreateEncryptionKey(t.Context(), params)
	require.NoError(t, err)

	_, err = client.CreateEncryptionKey(t.Context(), params)
	require.ErrorIs(t, err, ErrKeyExists)
}

func TestCreateEncryptionKey_ReconcilesUncertainCreate(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	ring.createUnavailable = true
	ring.versions[testEncryptionKeyName] = []string{testEncryptionKeyName + "/cryptoKeyVersions/1"}

	// gapic retries Unavailable; a short deadline exhausts them the way a real outage does.
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_, err := newFakeEncryptionClient(t, ring).CreateEncryptionKey(ctx, CreateEncryptionKeyParams{KeyRing: testKeyRing, KeyID: "orgkek-1"})
	require.ErrorContains(t, err, "may exist", "the lost create is reported, not hidden")
	require.Equal(t, []string{testEncryptionKeyName + "/cryptoKeyVersions/1"}, ring.disabledVersions())
}

func TestCreateEncryptionKey_RejectsBadInputsBeforeCalling(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	client := newFakeEncryptionClient(t, ring)

	_, err := client.CreateEncryptionKey(t.Context(), CreateEncryptionKeyParams{KeyRing: "bad", KeyID: "orgkek-1"})
	require.ErrorIs(t, err, ErrInvalidKeyRingName)

	_, err = client.CreateEncryptionKey(t.Context(), CreateEncryptionKeyParams{KeyRing: testKeyRing, KeyID: strings.Repeat("a", 64)})
	require.ErrorIs(t, err, ErrInvalidKeyID)
}

func TestDisableEncryptionKey_DisablesEveryEnabledVersion(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	ring.versions[testEncryptionKeyName] = []string{
		testEncryptionKeyName + "/cryptoKeyVersions/1",
		testEncryptionKeyName + "/cryptoKeyVersions/2",
	}

	require.NoError(t, newFakeEncryptionClient(t, ring).DisableEncryptionKey(t.Context(), testEncryptionKeyName))
	require.Equal(t, ring.versions[testEncryptionKeyName], ring.disabledVersions())
}

func TestGrantEncrypterDecrypter_BindsRoleOnTheKeyOnly(t *testing.T) {
	t.Parallel()

	ring := newFakeEncryptionRing()
	client := newFakeEncryptionClient(t, ring)

	require.NoError(t, client.GrantEncrypterDecrypter(t.Context(), testEncryptionKeyName, "Platform@Example.iam.gserviceaccount.com"))
	require.Equal(t, []string{"serviceAccount:platform@example.iam.gserviceaccount.com"}, ring.members(testEncryptionKeyName, string(EncrypterDecrypterRole)))
	require.Empty(t, ring.members(testKeyRing, string(EncrypterDecrypterRole)))

	require.NoError(t, client.RevokeEncrypterDecrypter(t.Context(), testEncryptionKeyName, "platform@example.iam.gserviceaccount.com"))
	require.Empty(t, ring.members(testEncryptionKeyName, string(EncrypterDecrypterRole)))
}

func TestGrantEncrypterDecrypter_RejectsRingAndVersionNames(t *testing.T) {
	t.Parallel()

	client := newFakeEncryptionClient(t, newFakeEncryptionRing())

	require.ErrorIs(t, client.GrantEncrypterDecrypter(t.Context(), testKeyRing, "sa@example.com"), ErrInvalidResourceName)
	require.ErrorIs(t, client.GrantEncrypterDecrypter(t.Context(), testEncryptionKeyName+"/cryptoKeyVersions/1", "sa@example.com"), ErrInvalidResourceName)
}
