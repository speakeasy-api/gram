package orgencryption_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func (ti *transitionInstance) decide(t *testing.T) orgencryption.Decision {
	t.Helper()

	d, err := ti.cipher.Redecide(t.Context(), orgencryption.Decision{OrganizationID: ti.orgID, UseCase: useCase, State: "", Generation: 0})
	require.NoError(t, err)

	return d
}

func TestCipher_PlatformKeyWritesOnlyThePlatformCopy(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	d := ti.decide(t)
	require.Equal(t, orgencryption.TransitionStatePlatformKey, d.State)

	copies, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("secret"))
	require.NoError(t, err)
	require.True(t, copies.Platform.Valid)
	require.False(t, copies.Org.Valid, "platform_key clears the organization-key copy")

	plaintext, err := ti.cipher.Open(t.Context(), d, fakeColumns, copies)
	require.NoError(t, err)
	require.Equal(t, "secret", string(plaintext))
}

func TestCipher_DualWriteWritesBothCopiesAndReadsTheOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	d := ti.decide(t)

	copies, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("secret"))
	require.NoError(t, err)
	require.True(t, copies.Platform.Valid)
	require.True(t, copies.Org.Valid)

	// A different platform-key copy proves which copy is read.
	copies.Platform = conv.ToPGText(ti.platformCopy(t, "stale"))
	plaintext, err := ti.cipher.Open(t.Context(), d, fakeColumns, copies)
	require.NoError(t, err)
	require.Equal(t, "secret", string(plaintext))
}

func TestCipher_DualWriteReadsThePlatformCopyOnlyWithoutAnOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	d := ti.decide(t)

	plaintext, err := ti.cipher.Open(t.Context(), d, fakeColumns, orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "old")), Org: orgencryption.ErasedCopies().Org})
	require.NoError(t, err)
	require.Equal(t, "old", string(plaintext))
}

func TestCipher_DualWriteFailsClosedOnAnOrganizationCopyThatDoesNotOpen(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	d := ti.decide(t)

	copies, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("secret"))
	require.NoError(t, err)

	moved := orgencryption.CopyColumns{UseCase: useCase, PlatformColumn: fakeColumns.PlatformColumn, OrgColumn: "other_table.value_org_encrypted"}
	_, err = ti.cipher.Open(t.Context(), d, moved, copies)
	require.ErrorIs(t, err, orgencryption.ErrUnreadable, "no fallback to the platform-key copy")
}

func TestCipher_DualWriteFailsClosedWhenTheOrganizationKeyIsSuspended(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	d := ti.decide(t)

	copies, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("secret"))
	require.NoError(t, err)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "test", orgencryption.SystemActor()))

	_, err = ti.cipher.Open(t.Context(), d, fakeColumns, copies)
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	require.NotErrorIs(t, err, orgencryption.ErrUnreadable, "a suspended key is transient, not unreadable")

	_, err = ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("secret"))
	require.ErrorIs(t, err, orgencryption.ErrSuspended)

	require.ErrorIs(t, ti.cipher.RequireUsable(t.Context(), d), orgencryption.ErrSuspended)
}

func TestCipher_OrgKMSReadsOnlyTheOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	_, err := ti.transitions.StartInOrgKMS(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.NoError(t, err)
	d := ti.decide(t)

	copies, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("secret"))
	require.NoError(t, err)
	require.False(t, copies.Platform.Valid)
	require.True(t, copies.Org.Valid)

	plaintext, err := ti.cipher.Open(t.Context(), d, fakeColumns, copies)
	require.NoError(t, err)
	require.Equal(t, "secret", string(plaintext))

	_, err = ti.cipher.Open(t.Context(), d, fakeColumns, orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "old")), Org: orgencryption.ErasedCopies().Org})
	require.ErrorIs(t, err, orgencryption.ErrUnreadable)

	_, err = ti.cipher.Open(t.Context(), d, fakeColumns, orgencryption.ErasedCopies())
	require.ErrorIs(t, err, orgencryption.ErrMissing)
}

func TestCipher_WithoutOrganizationEncryptionServesOnlyPlatformKey(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	platformOnly := orgencryption.NewCipher(testenv.NewLogger(t), ti.platform, nil, ti.transitions)

	d, err := platformOnly.Decide(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Equal(t, orgencryption.TransitionStatePlatformKey, d.State)

	ti.enroll(t)
	_, err = platformOnly.Redecide(t.Context(), d)
	require.ErrorIs(t, err, orgencryption.ErrOrganizationEncryptionUnavailable)
}

func TestCipher_RefusesADecisionHeldTooLong(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	now := time.Now()
	ti.cipher.SetClock(func() time.Time { return now })
	d := ti.decide(t)

	now = now.Add(orgencryption.MaxDecisionHold + time.Second)
	_, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("secret"))
	require.ErrorIs(t, err, orgencryption.ErrDecisionExpired)
	require.ErrorIs(t, ti.cipher.RequireUsable(t.Context(), d), orgencryption.ErrDecisionExpired, "the preflight refuses what Seal would refuse")

	fresh, err := ti.cipher.Redecide(t.Context(), d)
	require.NoError(t, err)
	_, err = ti.cipher.Seal(t.Context(), fresh, fakeColumns, []byte("secret"))
	require.NoError(t, err)
}

func TestCipher_RefusesAColumnOfAnotherUseCase(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	d := ti.decide(t)

	other := orgencryption.CopyColumns{UseCase: "other-use-case", PlatformColumn: fakeColumns.PlatformColumn, OrgColumn: fakeColumns.OrgColumn}
	_, err := ti.cipher.Seal(t.Context(), d, other, []byte("secret"))
	require.Error(t, err)
}

func TestCipher_FailsClosedOnADecisionWithAnUnknownState(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	d := orgencryption.Decision{OrganizationID: ti.orgID, UseCase: useCase, State: "", Generation: 0}

	_, err := ti.cipher.Seal(t.Context(), d, fakeColumns, []byte("secret"))
	require.ErrorIs(t, err, orgencryption.ErrInvalidState)

	require.ErrorIs(t, ti.cipher.RequireUsable(t.Context(), d), orgencryption.ErrInvalidState)

	_, err = ti.cipher.Open(t.Context(), d, fakeColumns, orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "old")), Org: orgencryption.ErasedCopies().Org})
	require.ErrorIs(t, err, orgencryption.ErrInvalidState)
}
