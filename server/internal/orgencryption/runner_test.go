package orgencryption_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
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
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.store.put(ti.orgID, "b", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "beta")), Org: orgencryption.ErasedCopies().Org})
	ti.store.put(ti.orgID, "broken", orgencryption.Copies{Platform: conv.ToPGText("not a ciphertext"), Org: orgencryption.ErasedCopies().Org})
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
	ti.store.put(ti.orgID, "c", orgencryption.Copies{Platform: conv.ToPGText(sealedWithRestoredKey), Org: orgencryption.ErasedCopies().Org})
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

func TestRunner_KeepsTheRecordOfAValueThatStillDoesNotDecrypt(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "broken", orgencryption.Copies{Platform: conv.ToPGText("not a ciphertext"), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)

	_, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	first, ok := ti.unreadableRecord(t, "broken")
	require.True(t, ok)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.Equal(t, int64(0), result.Converted)
	require.Equal(t, int64(1), result.Unreadable)

	second, ok := ti.unreadableRecord(t, "broken")
	require.True(t, ok, "a value that still does not decrypt stays recorded")
	require.True(t, second.LastDetectedAt.Time.After(first.LastDetectedAt.Time), "each retry records when it was last seen")
	require.Equal(t, first.FirstDetectedAt.Time, second.FirstDetectedAt.Time, "the same stored value keeps its first detection")
}

func TestRunner_RecordsNoVerificationWhenCopiesDiffer(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	d := ti.decide(t)

	mismatched, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("organization"))
	require.NoError(t, err)
	mismatched.Platform = conv.ToPGText(ti.platformCopy(t, "platform"))
	ti.store.put(ti.orgID, "m", mismatched)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Mismatched)
	require.False(t, result.VerificationPassed)

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Nil(t, transition.LastVerifiedAt)
}

// movingStore moves the transition to a new generation the first time
// verification lists sealed values, as a concurrent roll back and enroll would.
type movingStore struct {
	*fakeCopyStore

	ti    *transitionInstance
	moved bool
}

func (m *movingStore) ListSealed(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
	if !m.moved {
		m.moved = true
		if _, err := m.ti.transitions.Rollback(ctx, organizationID, useCase, orgencryption.SystemActor()); err != nil {
			return nil, fmt.Errorf("roll back during verification: %w", err)
		}
		if _, err := m.ti.transitions.Enroll(ctx, organizationID, useCase, orgencryption.SystemActor()); err != nil {
			return nil, fmt.Errorf("enroll during verification: %w", err)
		}
	}

	return m.fakeCopyStore.ListSealed(ctx, organizationID, after, limit)
}

func TestRunner_RecordsNoVerificationWhenTheStateMovesDuringIt(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{&movingStore{fakeCopyStore: ti.store, ti: ti, moved: false}})
	require.ErrorIs(t, err, orgencryption.ErrFenced)
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
