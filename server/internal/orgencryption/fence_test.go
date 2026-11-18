package orgencryption_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// seedFencedRow writes the row FencedWriteFixture updates.
func (ti *transitionInstance) seedFencedRow(t *testing.T) {
	t.Helper()

	err := repo.New(ti.conn).UpsertEncryptionUnreadableValue(t.Context(), repo.UpsertEncryptionUnreadableValueParams{
		OrganizationID:           ti.orgID,
		UseCase:                  string(useCase),
		SourceColumn:             fakeColumns.PlatformColumn,
		RowID:                    "fenced",
		PlatformCiphertextSha256: []byte("hash"),
		ErrorCode:                "ENC-4",
	})
	require.NoError(t, err)
}

func (ti *transitionInstance) fencedWrite(generation int64) repo.FencedWriteFixtureParams {
	return repo.FencedWriteFixtureParams{
		ErrorCode:         "written",
		OrganizationID:    ti.orgID,
		UseCase:           string(useCase),
		SourceColumn:      fakeColumns.PlatformColumn,
		RowID:             "fenced",
		DecidedGeneration: generation,
	}
}

// A writer's transaction that is still open holds the fence's lock on the
// transition row, so a transition waits until the write commits instead of
// committing underneath it.
func TestFence_AnOpenWriteHoldsTransitionsUntilItCommits(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	enrolled := ti.enroll(t)
	ti.seedFencedRow(t)

	writer := testenv.BeginTx(t, t.Context(), ti.conn)

	written, err := repo.New(writer).FencedWriteFixture(t.Context(), ti.fencedWrite(enrolled.Generation))
	require.NoError(t, err)
	require.Equal(t, int64(1), written)

	rolledBack := make(chan error, 1)
	go func() {
		_, err := ti.transitions.Rollback(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
		rolledBack <- err
	}()

	testenv.WaitForBackendsBlockedBy(t, t.Context(), ti.conn, testenv.BackendPID(writer), 1)
	require.NoError(t, writer.Commit(t.Context()))
	require.NoError(t, <-rolledBack)

	transition, err := ti.transitions.GetFresh(t.Context(), ti.orgID, useCase)
	require.NoError(t, err)
	require.Equal(t, enrolled.Generation+1, transition.Generation)
}

// A write that reaches the transition row while a transition holds it waits,
// then checks the committed generation, even though its statement started
// before the transition committed.
func TestFence_AWriteWaitingOnATransitionIsFencedWhenItCommits(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	enrolled := ti.enroll(t)
	ti.seedFencedRow(t)

	transition := testenv.BeginTx(t, t.Context(), ti.conn)

	q := repo.New(transition)
	locked, err := q.LockEncryptionTransition(t.Context(), repo.LockEncryptionTransitionParams{OrganizationID: ti.orgID, UseCase: string(useCase)})
	require.NoError(t, err)
	_, err = q.UpdateEncryptionTransitionState(t.Context(), repo.UpdateEncryptionTransitionStateParams{
		State:          string(orgencryption.TransitionStatePlatformKey),
		OrganizationID: ti.orgID,
		UseCase:        string(useCase),
		Generation:     locked.Generation,
	})
	require.NoError(t, err)

	type writeResult struct {
		rows int64
		err  error
	}
	staleWrite := make(chan writeResult, 1)
	go func() {
		rows, err := repo.New(ti.conn).FencedWriteFixture(t.Context(), ti.fencedWrite(enrolled.Generation))
		staleWrite <- writeResult{rows: rows, err: err}
	}()

	testenv.WaitForBackendsBlockedBy(t, t.Context(), ti.conn, testenv.BackendPID(transition), 1)
	require.NoError(t, transition.Commit(t.Context()))

	result := <-staleWrite
	require.NoError(t, result.err)
	require.Equal(t, int64(0), result.rows, "the write decided under the old generation is fenced")

	current, err := repo.New(ti.conn).FencedWriteFixture(t.Context(), ti.fencedWrite(enrolled.Generation+1))
	require.NoError(t, err)
	require.Equal(t, int64(1), current, "a write under the new generation lands")
}

// An organization with no transition row is at generation 0, so the
// predicate's COALESCE lets platform_key writes through.
func TestFence_AnOrganizationWithoutATransitionRowIsAtGenerationZero(t *testing.T) {
	t.Parallel()

	ti := newTransitionInstance(t)
	ti.seedFencedRow(t)

	written, err := repo.New(ti.conn).FencedWriteFixture(t.Context(), ti.fencedWrite(0))
	require.NoError(t, err)
	require.Equal(t, int64(1), written)

	fenced, err := repo.New(ti.conn).FencedWriteFixture(t.Context(), ti.fencedWrite(1))
	require.NoError(t, err)
	require.Equal(t, int64(0), fenced)
}
