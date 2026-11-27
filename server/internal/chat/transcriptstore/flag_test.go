package transcriptstore

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// countingProvisioner records EnsureUseCase calls and whether each ran while
// the test held a transaction open.
type countingProvisioner struct {
	next  Provisioner
	calls atomic.Int32
	inTx  atomic.Bool
	// sawTx is set when a call ran while inTx was set.
	sawTx atomic.Bool
	fail  atomic.Bool
}

func (p *countingProvisioner) EnsureUseCase(ctx context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error {
	p.calls.Add(1)
	if p.inTx.Load() {
		p.sawTx.Store(true)
	}
	if p.fail.Load() {
		return errors.New("example provisioning failure")
	}
	//nolint:wrapcheck // test double passes the real result through.
	return p.next.EnsureUseCase(ctx, organizationID, useCase, actor)
}

// erroringFlags fails every flag evaluation.
type erroringFlags struct{ feature.InMemory }

func (*erroringFlags) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, errors.New("example flag failure")
}

type flagFixture struct {
	*fixture
	flags       *feature.InMemory
	provisioner *countingProvisioner
	transcripts *Transcripts
}

// setupFlag builds a fixture whose organization has no transcript key yet,
// with the flag off.
func setupFlag(t *testing.T, listed ...uuid.UUID) *flagFixture {
	t.Helper()
	f := setup(t, false)
	ff := &flagFixture{fixture: f, flags: &feature.InMemory{}, provisioner: &countingProvisioner{next: f.keys.Provisioner}, transcripts: nil}
	ff.transcripts = NewTranscripts(f.store, listed...).WithFeatureFlag(ff.flags, ff.provisioner)
	return ff
}

func (f *flagFixture) setFlag(on bool) {
	f.flags.SetFlag(feature.FlagChatTranscriptShadow, f.org, on)
}

// write runs Prepare, then inserts the message in its own transaction, as
// every writer does.
func (f *flagFixture) write(t *testing.T, id uuid.UUID) {
	t.Helper()
	require.NoError(t, f.transcripts.Prepare(t.Context(), f.org, f.project))
	f.insertWithoutPrepare(t, id)
}

// insertWithoutPrepare inserts one message through InsertChatMessages in its
// own committed transaction, flagging any provisioning that runs inside it.
func (f *flagFixture) insertWithoutPrepare(t *testing.T, id uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	params := []chatrepo.CreateChatMessageParams{{
		ID: id, ChatID: f.chat, Role: "assistant", ProjectID: f.project,
		Content:      "sensitive example",
		ContentRaw:   []byte(`{"n":1e2,"large":9007199254740993}`),
		ToolCalls:    []byte(`[{"id":"example"}]`),
		StorageError: pgtype.Text{String: "", Valid: false},
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
	}}
	tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary is the behavior under test.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	f.provisioner.inTx.Store(true)
	defer f.provisioner.inTx.Store(false)
	_, err = f.transcripts.InsertChatMessages(ctx, tx, f.org, f.project, params)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

// sealed reports whether the message has a transcript.
func (f *flagFixture) sealed(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	_, err := fixturerepo.New(f.db).Ciphertext(t.Context(), id, f.project, f.org)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	require.NoError(t, err)
	return true
}

func (f *flagFixture) update(t *testing.T, id uuid.UUID, notes string) {
	t.Helper()
	require.NoError(t, f.transcripts.Prepare(t.Context(), f.org, f.project))
	f.provisioner.inTx.Store(true)
	defer f.provisioner.inTx.Store(false)
	_, err := f.updateBody(t, f.transcripts, f.chat, id, setNotes(notes))
	require.NoError(t, err)
}

func TestFlagOffKeepsProjectsPlaintext(t *testing.T) {
	t.Parallel()
	f := setupFlag(t)
	f.setFlag(false)
	id := uuid.New()
	f.write(t, id)

	require.Equal(t, ModePlaintext, f.transcripts.Mode(t.Context(), f.project))
	require.False(t, f.transcripts.MaySeal(t.Context(), f.project))
	require.False(t, f.sealed(t, id), "a flag-off project seals nothing")
	require.Zero(t, f.provisioner.calls.Load(), "a flag-off organization is never provisioned")
}

func TestFlagOnProvisionsBeforeTheWriteTransaction(t *testing.T) {
	t.Parallel()
	f := setupFlag(t)
	f.setFlag(true)

	require.Equal(t, ModePlaintext, f.transcripts.Mode(t.Context(), f.project), "shadow mode waits for the organization's key")
	require.True(t, f.transcripts.MaySeal(t.Context(), f.project), "a writer passes the organization so Prepare can provision it")
	id := uuid.New()
	f.write(t, id)
	require.Equal(t, ModeShadow, f.transcripts.Mode(t.Context(), f.project))
	require.True(t, f.sealed(t, id), "the first write after the flip is sealed")
	require.Equal(t, int32(1), f.provisioner.calls.Load())
	require.False(t, f.provisioner.sawTx.Load(), "provisioning never runs inside the write transaction")

	f.write(t, uuid.New())
	require.Equal(t, int32(1), f.provisioner.calls.Load(), "an organization is provisioned once per process")

	rows := []openRow{plaintextRow(id)}
	require.NoError(t, OpenChatMessages(t.Context(), f.transcripts, f.org, f.project, rows, openRowBody))
	require.Equal(t, "sensitive example", rows[0].content, "reads open the transcript")
}

// An insert that runs before any Prepare for a flag-enabled organization
// writes no transcript: the organization is plaintext until its key is
// provisioned outside a transaction.
func TestFlagOnInsertWithoutPrepareStaysPlaintext(t *testing.T) {
	t.Parallel()
	f := setupFlag(t)
	f.setFlag(true)
	id := uuid.New()
	f.insertWithoutPrepare(t, id)
	require.False(t, f.sealed(t, id))
	require.Zero(t, f.provisioner.calls.Load())
}

func TestFlagProvisioningFailureKeepsWritesWorking(t *testing.T) {
	t.Parallel()
	f := setupFlag(t)
	f.setFlag(true)
	f.provisioner.fail.Store(true)
	id := uuid.New()
	f.write(t, id)
	require.False(t, f.sealed(t, id), "an organization whose key cannot be provisioned stays plaintext")
	require.Equal(t, ModePlaintext, f.transcripts.Mode(t.Context(), f.project))

	f.provisioner.fail.Store(false)
	f.write(t, uuid.New())
	require.Equal(t, int32(1), f.provisioner.calls.Load(), "a failed organization is not retried at once")
}

// A message edited while the flag was off keeps its old transcript. Back on,
// reads serve the plaintext and the next update reseals it.
func TestFlagOffThenOnServesPlaintextForEditsMadeWhileOff(t *testing.T) {
	t.Parallel()
	f := setupFlag(t)
	f.setFlag(true)
	id := uuid.New()
	f.write(t, id)
	require.True(t, f.sealed(t, id))

	f.setFlag(false)
	f.update(t, id, "edited while off")
	_, err := f.store.Load(t.Context(), f.org, f.project, id)
	require.ErrorIs(t, err, ErrStale, "the plaintext update left the transcript behind")

	f.setFlag(true)
	rows := []openRow{plaintextRow(id)}
	require.NoError(t, OpenChatMessages(t.Context(), f.transcripts, f.org, f.project, rows, openRowBody))
	require.Equal(t, "edited while off", rows[0].notes.String, "a disagreeing transcript serves the plaintext")

	f.update(t, id, "edited while on")
	opened, err := f.store.Load(t.Context(), f.org, f.project, id)
	require.NoError(t, err, "the update reseals from the plaintext")
	require.Equal(t, "edited while on", *opened.ToolOutcomeNotes)
}

func TestFlagUnknownProjectIsPlaintext(t *testing.T) {
	t.Parallel()
	f := setupFlag(t)
	f.flags.SetFlag(feature.FlagChatTranscriptShadow, feature.AnyDistinctID, true)
	unknown := uuid.New()
	require.Equal(t, ModePlaintext, f.transcripts.Mode(t.Context(), unknown))
	require.False(t, f.transcripts.MaySeal(t.Context(), unknown))
	require.Zero(t, f.provisioner.calls.Load())
}

func TestFlagLookupErrorsAreTreatedAsPlaintext(t *testing.T) {
	t.Parallel()
	f := setup(t, false)
	provisioner := &countingProvisioner{next: f.keys.Provisioner}
	failing := &erroringFlags{}
	failing.SetFlag(feature.FlagChatTranscriptShadow, feature.AnyDistinctID, true)
	transcripts := NewTranscripts(f.store).WithFeatureFlag(failing, provisioner)
	require.Equal(t, ModePlaintext, transcripts.Mode(t.Context(), f.project), "a failing flag evaluation is off")

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagChatTranscriptShadow, f.org, true)
	transcripts = NewTranscripts(f.store).WithFeatureFlag(flags, provisioner)
	require.Equal(t, ModePlaintext, transcripts.Mode(cancelled, f.project), "a failing organization lookup is plaintext")
	require.Zero(t, provisioner.calls.Load())
}

func TestShadowListIgnoresTheFlag(t *testing.T) {
	t.Parallel()
	f := setupFlag(t)
	f.transcripts = NewTranscripts(f.store, f.project).WithFeatureFlag(f.flags, f.provisioner)
	f.setFlag(false)
	require.Equal(t, ModeShadow, f.transcripts.Mode(t.Context(), f.project), "a listed project is in shadow mode with the flag off")
	require.Error(t, f.transcripts.Prepare(t.Context(), f.org, f.project), "a listed project fails closed without its key")
	require.Zero(t, f.provisioner.calls.Load(), "listed organizations are provisioned at startup, not by the flag")
}

// Reads never provision a key. Before any writer provisioned the organization
// they serve plaintext; once another process sealed messages, they open them
// without provisioning anything themselves.
func TestFlagReadsNeverProvision(t *testing.T) {
	t.Parallel()
	f := setupFlag(t)
	f.setFlag(true)
	id := uuid.New()
	f.insertWithoutPrepare(t, id)

	reader := NewTranscripts(f.store).WithFeatureFlag(f.flags, f.provisioner)
	rows := []openRow{plaintextRow(id)}
	require.NoError(t, OpenChatMessages(t.Context(), reader, f.org, f.project, rows, openRowBody))
	require.False(t, f.sealed(t, id))
	require.Equal(t, "sensitive example", rows[0].content, "an organization without a key is served from plaintext")
	require.Zero(t, f.provisioner.calls.Load(), "a read does not provision the key")

	sealed := uuid.New()
	f.write(t, sealed)
	require.Equal(t, int32(1), f.provisioner.calls.Load(), "the writer provisions the key")

	rows = []openRow{plaintextRow(sealed)}
	require.NoError(t, OpenChatMessages(t.Context(), reader, f.org, f.project, rows, openRowBody))
	require.Equal(t, "sensitive example", rows[0].content, "a reader opens transcripts a writer sealed")
	require.Equal(t, int32(1), f.provisioner.calls.Load(), "the reader still provisioned nothing")
	require.Equal(t, ModePlaintext, reader.Mode(t.Context(), f.project), "the reader's process never writes for the organization")
}
