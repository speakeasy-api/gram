package orgencryption_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

func newIdentity() *gcpauth.Identity {
	return gcpauth.NewIdentity(gcpauth.NewStubResolver())
}

func TestEncryptor_RoundTripsAProvisionedValue(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)
	encryptor := ti.encryptor(t)

	ciphertext, err := encryptor.Encrypt(t.Context(), column, []byte("node state"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(ciphertext, encryption.EnvelopePrefix))
	require.NotContains(t, ciphertext, "node state")

	plaintext, err := encryptor.Decrypt(t.Context(), column, ciphertext)
	require.NoError(t, err)
	require.Equal(t, "node state", string(plaintext))
}

func TestEncryptor_FailsClosedWithoutProvisioning(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	_, err := ti.encryptor(t).Encrypt(t.Context(), column, []byte("secret"))
	require.ErrorIs(t, err, orgencryption.ErrNotProvisioned)
}

func TestEncryptor_RefusesAValueMovedToAnotherColumn(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)
	encryptor := ti.encryptor(t)

	ciphertext, err := encryptor.Encrypt(t.Context(), column, []byte("secret"))
	require.NoError(t, err)

	_, err = encryptor.Decrypt(t.Context(), "other_table.value_encrypted", ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext)
}

func TestEncryptor_RefusesAValueMovedToAnotherOrganization(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	ciphertext, err := ti.encryptor(t).Encrypt(t.Context(), column, []byte("secret"))
	require.NoError(t, err)

	otherOrg := createOrganization(t, ti.conn)
	require.NoError(t, ti.fixture.Provisioner.EnsureUseCase(t.Context(), otherOrg, useCase, orgencryption.SystemActor()))
	other, err := ti.fixture.Service.For(otherOrg, useCase)
	require.NoError(t, err)

	_, err = other.Decrypt(t.Context(), column, ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext)
}

func TestEncryptor_RefusesApplicationKeyCiphertext(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	_, err := ti.encryptor(t).Decrypt(t.Context(), column, "c2VjcmV0")
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext)
}

func TestService_UnwrapsADataKeyOncePerCachePeriod(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	ciphertext, err := ti.encryptor(t).Encrypt(t.Context(), column, []byte("secret"))
	require.NoError(t, err)

	service := ti.freshService(t)
	encryptor, err := service.For(ti.orgID, useCase)
	require.NoError(t, err)

	before := ti.fixture.KMS.Decrypts()
	for range 5 {
		_, err := encryptor.Decrypt(t.Context(), column, ciphertext)
		require.NoError(t, err)
		_, err = encryptor.Encrypt(t.Context(), column, []byte("more"))
		require.NoError(t, err)
	}
	require.Equal(t, before+1, ti.fixture.KMS.Decrypts(), "the DEK is unwrapped once and then served from memory")
}

func TestService_RefusesUnknownUseCase(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	_, err := ti.fixture.Service.For(ti.orgID, orgencryption.UseCase("not-registered"))
	require.ErrorIs(t, err, orgencryption.ErrUnknownUseCase)

	err = ti.fixture.Provisioner.EnsureUseCase(t.Context(), ti.orgID, orgencryption.UseCase("not-registered"), orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrUnknownUseCase)
}

func TestEncryptor_FailsClosedWhileSuspended(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	ciphertext, err := ti.encryptor(t).Encrypt(t.Context(), column, []byte("secret"))
	require.NoError(t, err)

	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err = ti.encryptor(t).Encrypt(t.Context(), column, []byte("secret"))
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	_, err = ti.encryptor(t).Decrypt(t.Context(), column, ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrSuspended)

	require.NoError(t, ti.fixture.Provisioner.Resume(t.Context(), ti.orgID, orgencryption.SystemActor()))

	plaintext, err := ti.encryptor(t).Decrypt(t.Context(), column, ciphertext)
	require.NoError(t, err)
	require.Equal(t, "secret", string(plaintext))
}
