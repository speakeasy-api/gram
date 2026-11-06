package orgencryption_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

func (ti *testInstance) config(t *testing.T) repo.OrganizationEncryptionConfig {
	t.Helper()

	config, err := repo.New(ti.conn).GetOrganizationEncryptionConfig(t.Context(), ti.orgID)
	require.NoError(t, err)

	return config
}

// keyResource is the KMS resource name a KEK row records.
func (ti *testInstance) keyResource(t *testing.T, keyID uuid.UUID) string {
	t.Helper()

	row, err := repo.New(ti.conn).GetEncryptionKeyForUse(t.Context(), repo.GetEncryptionKeyForUseParams{ID: keyID, OrganizationID: ti.orgID})
	require.NoError(t, err)

	return row.ResourceName.String
}

func encryptionEnvelope(ciphertext string) (uuid.UUID, error) {
	envelope, err := encryption.ParseEnvelope(ciphertext)
	return envelope.DataKeyID, err //nolint:wrapcheck // test helper
}

func (ti *testInstance) wrapsUnder(t *testing.T, keyID uuid.UUID) int {
	t.Helper()

	rows, err := repo.New(ti.conn).ListDataEncryptionKeyWrapsForKey(t.Context(), repo.ListDataEncryptionKeyWrapsForKeyParams{ExternalKeyID: keyID, OrganizationID: ti.orgID})
	require.NoError(t, err)

	wrapped := 0
	for _, row := range rows {
		if row.WrapID.Valid {
			wrapped++
		}
	}

	return wrapped
}

func TestEnsureUseCase_MintsAManagedKeyAndADataKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	configsBefore, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionOrganizationEncryptionConfigCreate)
	require.NoError(t, err)
	keysBefore, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionDataEncryptionKeyCreate)
	require.NoError(t, err)

	ti.provision(t)

	config := ti.config(t)
	require.Equal(t, string(orgencryption.ModeSpeakeasyManaged), config.Mode)
	require.Equal(t, string(orgencryption.ConfigStateActive), config.State)
	require.Contains(t, ti.keyResource(t, config.KekExternalKeyID), orgencryptiontest.KeyRing+"/cryptoKeys/orgkek-")
	require.Equal(t, 1, ti.wrapsUnder(t, config.KekExternalKeyID))

	configsAfter, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionOrganizationEncryptionConfigCreate)
	require.NoError(t, err)
	require.Equal(t, configsBefore+1, configsAfter)
	keysAfter, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionDataEncryptionKeyCreate)
	require.NoError(t, err)
	require.Equal(t, keysBefore+1, keysAfter)
}

func TestEnsureUseCase_IsIdempotentUnderConcurrency(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() {
			errs[i] = ti.fixture.Provisioner.EnsureUseCase(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
		})
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		require.ErrorIs(t, err, orgencryption.ErrConcurrentChange)
	}
	require.Positive(t, succeeded)
	ti.provision(t)

	readable, err := repo.New(ti.conn).ListReadableDataEncryptionKeys(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Len(t, readable, 1, "concurrent provisioning settles on one DEK")
}

func TestRotateDataKey_KeepsOldValuesReadable(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	old, err := ti.encryptor(t).Encrypt(t.Context(), column, []byte("before rotation"))
	require.NoError(t, err)

	require.NoError(t, ti.fixture.Provisioner.RotateDataKey(t.Context(), ti.orgID, useCase, orgencryption.SystemActor()))

	fresh, err := ti.encryptor(t).Encrypt(t.Context(), column, []byte("after rotation"))
	require.NoError(t, err)

	oldEnvelope, err := encryptionEnvelope(old)
	require.NoError(t, err)
	freshEnvelope, err := encryptionEnvelope(fresh)
	require.NoError(t, err)
	require.NotEqual(t, oldEnvelope, freshEnvelope, "new values use the new DEK")

	plaintext, err := ti.encryptor(t).Decrypt(t.Context(), column, old)
	require.NoError(t, err)
	require.Equal(t, "before rotation", string(plaintext))
}

func TestSwitchKey_MovesToACustomerKeyWithoutRewritingValues(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)
	managed := ti.config(t).KekExternalKeyID

	ciphertext, err := ti.encryptor(t).Encrypt(t.Context(), column, []byte("secret"))
	require.NoError(t, err)

	customer := orgencryptiontest.CreateCustomerKey(t, ti.conn, ti.orgID, "customer-kek")
	switched, err := ti.fixture.Provisioner.SwitchKey(t.Context(), ti.orgID, customer, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, string(orgencryption.ConfigStateSwitching), switched.State)
	require.Equal(t, string(orgencryption.ModeCustomerManaged), switched.Mode)
	require.Equal(t, 1, ti.wrapsUnder(t, managed), "the previous KEK keeps its wraps during the switch")
	require.Equal(t, 1, ti.wrapsUnder(t, customer))

	// With the managed KEK gone, the value still decrypts through the new one.
	require.NoError(t, ti.fixture.KMS.DisableEncryptionKey(t.Context(), ti.keyResource(t, managed)))
	plaintext, err := ti.freshDecrypt(t, ciphertext)
	require.NoError(t, err)
	require.Equal(t, "secret", string(plaintext))

	completed, err := ti.fixture.Provisioner.CompleteSwitch(t.Context(), ti.orgID, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, string(orgencryption.ConfigStateActive), completed.State)
	require.False(t, completed.PreviousKekExternalKeyID.Valid)
	require.Zero(t, ti.wrapsUnder(t, managed))

	disabled, err := ti.fixture.Provisioner.SweepManagedKeys(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, disabled, "the unused managed KEK is disabled")
}

func (ti *testInstance) freshDecrypt(t *testing.T, ciphertext string) ([]byte, error) {
	t.Helper()

	encryptor, err := ti.freshService(t).For(ti.orgID, useCase)
	require.NoError(t, err)

	return encryptor.Decrypt(t.Context(), column, ciphertext) //nolint:wrapcheck // the test asserts on the service's own error
}

func TestSwitchKey_RollbackRestoresThePreviousKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)
	managed := ti.config(t).KekExternalKeyID

	customer := orgencryptiontest.CreateCustomerKey(t, ti.conn, ti.orgID, "customer-kek")
	_, err := ti.fixture.Provisioner.SwitchKey(t.Context(), ti.orgID, customer, orgencryption.SystemActor())
	require.NoError(t, err)

	rolledBack, err := ti.fixture.Provisioner.RollbackSwitch(t.Context(), ti.orgID, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, managed, rolledBack.KekExternalKeyID)
	require.Equal(t, string(orgencryption.ModeSpeakeasyManaged), rolledBack.Mode)
	require.Equal(t, string(orgencryption.ConfigStateActive), rolledBack.State)
	require.Zero(t, ti.wrapsUnder(t, customer))

	disabled, err := ti.fixture.Provisioner.SweepManagedKeys(t.Context(), 10)
	require.NoError(t, err)
	require.Zero(t, disabled, "the managed KEK is still in use")
}

func TestSwitchKey_RefusesAKeyThatCannotEncrypt(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	customer := orgencryptiontest.CreateCustomerKey(t, ti.conn, ti.orgID, "disabled-kek")
	require.NoError(t, ti.fixture.KMS.DisableEncryptionKey(t.Context(), orgencryptiontest.CustomerKeyName("disabled-kek")))

	_, err := ti.fixture.Provisioner.SwitchKey(t.Context(), ti.orgID, customer, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrInvalidKey)
	require.Equal(t, string(orgencryption.ConfigStateActive), ti.config(t).State)
}

func TestSwitchKey_RefusesAnotherOrganizationsKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	otherOrg := createOrganization(t, ti.conn)
	foreign := orgencryptiontest.CreateCustomerKey(t, ti.conn, otherOrg, "foreign-kek")

	_, err := ti.fixture.Provisioner.SwitchKey(t.Context(), ti.orgID, foreign, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrInvalidKey)
}

func TestCompleteSwitch_RefusesWhenNoSwitchIsInFlight(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	_, err := ti.fixture.Provisioner.CompleteSwitch(t.Context(), ti.orgID, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrInvalidState)
}

func TestCheck_SuspendsWhenTheKeyIsDisabledAndCompletesAfterGrace(t *testing.T) {
	t.Parallel()

	ti := newTestInstanceWithConfig(t, orgencryption.ProvisionerConfig{
		Keys:              orgencryption.KeyConfig{KeyRing: "", PlatformCredentialID: uuid.Nil, PlatformServiceAccount: ""},
		SwitchGracePeriod: time.Nanosecond,
	})
	ti.provision(t)

	customer := orgencryptiontest.CreateCustomerKey(t, ti.conn, ti.orgID, "customer-kek")
	_, err := ti.fixture.Provisioner.SwitchKey(t.Context(), ti.orgID, customer, orgencryption.SystemActor())
	require.NoError(t, err)

	require.NoError(t, ti.fixture.Provisioner.Check(t.Context(), ti.orgID))
	require.Equal(t, string(orgencryption.ConfigStateActive), ti.config(t).State, "a clean probe past the grace period completes the switch")

	require.NoError(t, ti.fixture.KMS.DisableEncryptionKey(t.Context(), orgencryptiontest.CustomerKeyName("customer-kek")))
	require.Error(t, ti.fixture.Provisioner.Check(t.Context(), ti.orgID))

	config := ti.config(t)
	require.Equal(t, string(orgencryption.ConfigStateSuspended), config.State)
	require.True(t, config.SuspendedReason.Valid)
	require.True(t, config.LastKekCheckError.Valid)

	_, err = ti.encryptor(t).Encrypt(t.Context(), column, []byte("secret"))
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
}

func TestCheckDue_ChecksEachOrganizationOnce(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.provision(t)

	checked, err := ti.fixture.Provisioner.CheckDue(t.Context(), time.Hour, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, checked)

	checked, err = ti.fixture.Provisioner.CheckDue(t.Context(), time.Hour, 100, time.Minute)
	require.NoError(t, err)
	require.Zero(t, checked, "a fresh check is not due again")
	require.False(t, ti.config(t).LastKekCheckError.Valid)
}
