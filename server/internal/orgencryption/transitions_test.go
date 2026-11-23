package orgencryption_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestTransitions_AnOrganizationWithoutARowIsInPlatformKey(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)

	transition, err := ti.transitions.Get(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Equal(t, orgencryption.TransitionStatePlatformKey, transition.State)
	require.Equal(t, int64(0), transition.Generation)
}

func TestTransitions_EnrollProvisionsAndMovesToDualWrite(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionOrganizationEncryptionTransitionEnroll)
	require.NoError(t, err)

	transition := ti.enroll(t)
	require.Equal(t, orgencryption.TransitionStateDualWrite, transition.State)
	require.Equal(t, int64(1), transition.Generation)

	encryptor := ti.encryptor(t)
	require.NoError(t, encryptor.RequireUsable(t.Context()), "enroll provisions the use case's data key")

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionOrganizationEncryptionTransitionEnroll)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionOrganizationEncryptionTransitionEnroll)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "dual_write", snapshot["state"])
}

func TestTransitions_AuditEntriesAreInternalAndPublishNothing(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	enrolled := ti.enroll(t)

	before, err := testrepo.New(ti.conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)

	_, err = ti.transitions.Rollback(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.NoError(t, err)

	after, err := testrepo.New(ti.conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Equal(t, before, after, "transition events never reach a webhook")

	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionOrganizationEncryptionTransitionRollback)
	require.NoError(t, err)
	require.Equal(t, enrolled.ID.String(), record.SubjectID)

	internal, err := auditrepo.New(ti.conn).GetAuditLogInternalBySubject(t.Context(), enrolled.ID.String())
	require.NoError(t, err)
	require.True(t, internal)
}

func TestTransitions_EnrollTwiceIsRefused(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)

	_, err := ti.transitions.Enroll(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
}

func TestTransitions_RollbackAndEnrollAgainKeepIncreasingTheGeneration(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)

	rolledBack, err := ti.transitions.Rollback(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, orgencryption.TransitionStatePlatformKey, rolledBack.State)
	require.Equal(t, int64(2), rolledBack.Generation)

	reenrolled := ti.enroll(t)
	require.Equal(t, orgencryption.TransitionStateDualWrite, reenrolled.State)
	require.Equal(t, int64(3), reenrolled.Generation)
}

func TestTransitions_RollbackFromPlatformKeyIsRefused(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)

	_, err := ti.transitions.Rollback(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
}

func TestTransitions_ChangesNeedAProvisionerAndAuditLogger(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	readOnly := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, nil, nil, orgencryption.TransitionsConfig{ClearSettleDelay: 0})

	_, err := readOnly.Enroll(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.Error(t, err)

	transition, err := readOnly.Get(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Equal(t, orgencryption.TransitionStatePlatformKey, transition.State)
}

func TestTransitions_StartInOrgKMSMarksPlatformCopiesCleared(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)

	transition, err := ti.transitions.StartInOrgKMS(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, orgencryption.TransitionStateOrgKMS, transition.State)
	require.NotNil(t, transition.PlatformCopiesClearedAt)

	_, err = ti.transitions.StartInOrgKMS(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)

	_, err = ti.transitions.Rollback(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed, "nothing leaves org_kms")
}

func TestTransitions_FinalizeRequiresTheMinimumTimeInDualWrite(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	_, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, useCase, orgencryption.FinalizeRequest{MinimumDualWrite: 0, Stores: []orgencryption.CopyStore{ti.store}}, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrFinalizeBlocked, "the floor applies even when the caller asks for no minimum")
}

func TestTransitions_FinalizeRequiresAVerificationPass(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	ti.backdate(t, time.Hour)

	_, err := ti.transitions.Finalize(t.Context(), ti.orgID, useCase, orgencryption.FinalizeRequest{MinimumDualWrite: 0, Stores: []orgencryption.CopyStore{ti.store}}, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrFinalizeBlocked)
}

func TestTransitions_FinalizeRefusesWhileValuesWait(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)
	_, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	ti.backdate(t, time.Hour)

	// Written by a stale platform_key writer after the run.
	ti.store.put(ti.orgID, "late", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "late")), Org: orgencryption.ErasedCopies().Org})

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, useCase, orgencryption.FinalizeRequest{MinimumDualWrite: 0, Stores: []orgencryption.CopyStore{ti.store}}, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrFinalizeBlocked)
}

func TestTransitions_FinalizeThenClearDestroysPlatformCopies(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.store.put(ti.orgID, "a", orgencryption.Copies{Platform: conv.ToPGText(ti.platformCopy(t, "alpha")), Org: orgencryption.ErasedCopies().Org})
	ti.store.put(ti.orgID, "broken", orgencryption.Copies{Platform: conv.ToPGText("not a ciphertext"), Org: orgencryption.ErasedCopies().Org})
	ti.enroll(t)

	result, err := ti.runner.Run(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store})
	require.NoError(t, err)
	require.True(t, result.VerificationPassed)
	ti.backdate(t, time.Hour)

	finalized, err := ti.transitions.Finalize(t.Context(), ti.orgID, useCase, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: []orgencryption.CopyStore{ti.store}}, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, orgencryption.TransitionStateOrgKMS, finalized.State)
	require.Nil(t, finalized.PlatformCopiesClearedAt, "finalize clears nothing itself")

	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionOrganizationEncryptionTransitionFinalize)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.InDelta(t, orgencryption.MinimumDualWriteFloor.Seconds(), metadata["minimum_dual_write_seconds"], 0, "the floor is recorded when it exceeds the request")
	require.InDelta(t, 1, metadata["unreadable_values"], 0)

	cleared, err := ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store}, orgencryption.SystemActor())
	require.NoError(t, err)
	require.NotNil(t, cleared.PlatformCopiesClearedAt)
	require.False(t, ti.store.get("a").Platform.Valid)
	require.True(t, ti.store.get("a").Org.Valid)
	require.False(t, ti.store.get("broken").Platform.Valid, "unreadable values are destroyed with the rest")

	again, err := ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store}, orgencryption.SystemActor())
	require.NoError(t, err)
	require.Equal(t, cleared.PlatformCopiesClearedAt, again.PlatformCopiesClearedAt, "clearing again is a no-op")

	_, err = ti.transitions.Rollback(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
}

func TestTransitions_ClearRefusesBeforeFinalize(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.enroll(t)

	_, err := ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, useCase, []orgencryption.CopyStore{ti.store}, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
}
