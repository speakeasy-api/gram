package orgencryption_test

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func (ti *transitionInstance) unreadableRecord(t *testing.T, rowID string) (repo.OrganizationEncryptionUnreadableValue, bool) {
	t.Helper()

	record, err := repo.New(ti.conn).GetEncryptionUnreadableValue(t.Context(), repo.GetEncryptionUnreadableValueParams{
		OrganizationID: ti.orgID,
		UseCase:        string(useCase),
		SourceColumn:   fakeColumns.PlatformColumn,
		RowID:          rowID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.OrganizationEncryptionUnreadableValue{}, false
	}
	require.NoError(t, err)

	return record, true
}

func TestRunner_ConvertsVerifiesAndRecordsTheRun(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put("a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.store.put("b", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "beta")), Org: orgencryption.ErasedCopies().Org})
	ti.store.put("broken", orgencryption.Copies{Platform: conv.ToPGText("not a ciphertext"), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Converted)
	require.Equal(t, int64(0), result.Remaining)
	require.Equal(t, int64(1), result.Unreadable)
	require.Equal(t, int64(2), result.Verified)
	require.True(t, result.VerificationPassed)

	d := ti.decide(t)
	plaintext, err := ti.cipher.Open(t.Context(), d, fakeColumns, orgencryption.Copies{Platform: orgencryption.ErasedCopies().Platform, Org: ti.store.get("a").Org})
	require.NoError(t, err, "the organization-key copy opens on its own")
	require.Equal(t, "alpha", string(plaintext))

	record, ok := ti.unreadableRecord(t, "broken")
	require.True(t, ok)
	hash := sha256.Sum256([]byte("not a ciphertext"))
	require.Equal(t, hash[:], record.PlatformCiphertextSha256)
	require.NotEmpty(t, record.ErrorCode)

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.NotNil(t, transition.LastBackfill.CompletedAt)
	require.Equal(t, int64(2), *transition.LastBackfill.Converted)
	require.Equal(t, int64(1), *transition.LastBackfill.Unreadable)
	require.Nil(t, transition.LastBackfill.Error)
	require.NotNil(t, transition.LastVerifiedAt)
}

func TestRunner_RetriesUnreadableValuesAndForgetsThemOnceTheyOpen(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	restored, err := encryption.NewWithBytes(key)
	require.NoError(t, err)

	sealedWithRestoredKey, err := restored.Encrypt([]byte("gamma"))
	require.NoError(t, err)
	ti.store.put("c", orgencryption.Copies{Platform: conv.ToPGText(sealedWithRestoredKey), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)

	_, err = ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	_, ok := ti.unreadableRecord(t, "c")
	require.True(t, ok, "the current platform key cannot open it")

	restoredCipher := orgencryption.NewCipher(testenv.NewLogger(t), restored, ti.fixture.Service, ti.transitions)
	restoredRunner := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, restoredCipher, testenv.NewMeterProvider(t))
	result, err := restoredRunner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store.withPlatformKey(restored)})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Converted)

	_, ok = ti.unreadableRecord(t, "c")
	require.False(t, ok, "a value that seals is no longer recorded as unreadable")
}

func TestRunner_RecordsNoVerificationWhenCopiesDiffer(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	d := ti.decide(t)

	mismatched, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("organization"))
	require.NoError(t, err)
	mismatched.Platform = conv.ToPGText(ti.platformCopy(t, "platform"))
	ti.store.put("m", mismatched)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Mismatched)
	require.False(t, result.VerificationPassed)

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Nil(t, transition.LastVerifiedAt)
}

func TestRunner_RunsOnlyInDualWrite(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)

	_, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
}
