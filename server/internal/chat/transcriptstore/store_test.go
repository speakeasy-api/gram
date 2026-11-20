package transcriptstore

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/stretchr/testify/require"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatal(err)
	}
	infra = env
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Print(err)
		code = 1
	}
	os.Exit(code)
}

type fixture struct {
	db            *pgxpool.Pool
	keys          *orgencryptiontest.Fixture
	store         *Store
	org           string
	project, chat uuid.UUID
}

func setup(t *testing.T, provision bool) *fixture {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, "transcripts")
	require.NoError(t, err)
	f := &fixture{db: db, org: "org_" + uuid.NewString(), project: uuid.New(), chat: uuid.New()}
	_, err = fixturerepo.New(db).CreateOrganization(t.Context(), f.org)
	require.NoError(t, err)
	_, err = fixturerepo.New(db).CreateProject(t.Context(), f.project, f.org)
	require.NoError(t, err)
	_, err = fixturerepo.New(db).CreateChat(t.Context(), f.chat, f.project, f.org)
	require.NoError(t, err)
	f.keys = orgencryptiontest.New(t, db)
	f.store, err = New(db, f.keys.Service)
	require.NoError(t, err)
	if provision {
		require.NoError(t, f.keys.Provisioner.EnsureUseCase(t.Context(), f.org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	}
	return f
}
func (f *fixture) add(t *testing.T, id uuid.UUID) {
	t.Helper()
	_, err := fixturerepo.New(f.db).CreateMessage(t.Context(), id, f.chat, project(f.project))
	require.NoError(t, err)
}
func (f *fixture) capture(t *testing.T, ids ...uuid.UUID) {
	t.Helper()
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, f.store.Capture(t.Context(), tx, f.org, f.project, ids))
	require.NoError(t, tx.Commit(t.Context()))
}
func TestCaptureWithOneConnectionAndColdKeys(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	first, second := uuid.New(), uuid.New()
	f.add(t, first)
	f.add(t, second)
	cfg := f.db.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	service, err := orgencryption.NewService(testenv.NewLogger(t), pool, gcpauth.NewIdentity(gcpauth.NewStubResolver()), f.keys.KMS.Clients, f.keys.Keys)
	require.NoError(t, err)
	store, err := New(pool, service)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	before := f.keys.KMS.Decrypts()
	require.NoError(t, store.Capture(ctx, tx, f.org, f.project, []uuid.UUID{first, second}))
	require.Equal(t, before+1, f.keys.KMS.Decrypts(), "a batch reuses its transaction-local DEK")
	require.NoError(t, tx.Commit(ctx))
	opened, err := store.Load(ctx, f.org, f.project, first)
	require.NoError(t, err)
	require.Equal(t, "sensitive example", opened.Content)
}

func TestCaptureLoadOwnershipAndStaleness(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	var before string
	before, err := fixturerepo.New(f.db).MessageSnapshot(t.Context(), id, project(f.project))
	require.NoError(t, err)
	f.capture(t, id, id)
	var after, ciphertext string
	after, err = fixturerepo.New(f.db).MessageSnapshot(t.Context(), id, project(f.project))
	require.NoError(t, err)
	require.Equal(t, before, after)
	ciphertext, err = fixturerepo.New(f.db).Ciphertext(t.Context(), id, f.project, f.org)
	require.NoError(t, err)
	require.NotContains(t, ciphertext, "sensitive example")
	opened, err := f.store.Load(t.Context(), f.org, f.project, id)
	require.NoError(t, err)
	require.Equal(t, "sensitive example", opened.Content)
	for _, target := range []struct {
		org     string
		project uuid.UUID
	}{{"org_other", f.project}, {f.org, uuid.New()}} {
		_, err = f.store.Load(t.Context(), target.org, target.project, id)
		require.ErrorIs(t, err, ErrNotFound)
		tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
		require.NoError(t, err)
		require.ErrorIs(t, f.store.Capture(t.Context(), tx, target.org, target.project, []uuid.UUID{id}), ErrNotFound)
		require.NoError(t, tx.Rollback(t.Context()))
	}
	_, err = fixturerepo.New(f.db).ChangeContent(t.Context(), id, project(f.project))
	require.NoError(t, err)
	opened, err = f.store.Load(t.Context(), f.org, f.project, id)
	require.ErrorIs(t, err, ErrStale)
	require.Equal(t, transcriptcrypto.Message{}, opened)
	// Even a forged matching hash cannot make an authenticated older body current.
	_, err = fixturerepo.New(f.db).ForgeSourceHash(t.Context(), id, project(f.project), f.org)
	require.NoError(t, err)
	opened, err = f.store.Load(t.Context(), f.org, f.project, id)
	require.ErrorIs(t, err, ErrStale)
	require.Equal(t, transcriptcrypto.Message{}, opened)
	f.capture(t, id)
	opened, err = f.store.Load(t.Context(), f.org, f.project, id)
	require.NoError(t, err)
	require.Equal(t, "changed", opened.Content)
	// Replacing a ciphertext with another valid identity is not accepted.
	other := uuid.New()
	f.add(t, other)
	f.capture(t, other)
	_, err = fixturerepo.New(f.db).ReplaceCiphertext(t.Context(), ciphertext, other, f.project, f.org)
	require.NoError(t, err)
	_, err = f.store.Load(t.Context(), f.org, f.project, other)
	require.Error(t, err)
}
func TestCASRejectsChangedSnapshotAndLocksLiveSource(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	q := repo.New(f.db)
	snapshot, err := q.Snapshot(t.Context(), repo.SnapshotParams{MessageID: id, ProjectID: project(f.project), OrganizationID: f.org})
	require.NoError(t, err)
	_, err = fixturerepo.New(f.db).ChangeNotes(t.Context(), id, project(f.project))
	require.NoError(t, err)
	n, err := q.Capture(t.Context(), repo.CaptureParams{ProjectID: f.project, OrganizationID: f.org, MessageID: id, ChatID: f.chat, Ciphertext: "not-used", SourceHash: snapshot.SourceHash})
	require.NoError(t, err)
	require.Zero(t, n)
	// Capture's shared source lock lasts until the caller commits. A concurrent
	// source edit cannot race ahead of the CAS and make it overwrite newer work.
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, f.store.Capture(t.Context(), tx, f.org, f.project, []uuid.UUID{id}))
	edit, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = edit.Rollback(t.Context()) }()
	_, err = edit.Exec(t.Context(), `SET LOCAL lock_timeout='100ms'`) //nolint:glint // notestingrawsql: transaction-local lock timeout is part of the locking protocol under test.
	require.NoError(t, err)
	_, err = fixturerepo.New(edit).RaceContent(t.Context(), id, project(f.project))
	require.Error(t, err)
	require.NoError(t, edit.Rollback(t.Context()))
	require.NoError(t, tx.Commit(t.Context()))
	_, err = f.store.Load(t.Context(), f.org, f.project, id)
	require.NoError(t, err)
}
func TestBoundedResumableBackfillAndProgress(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ids := []uuid.UUID{uuid.MustParse("00000000-0000-0000-0000-000000000003"), uuid.MustParse("00000000-0000-0000-0000-000000000002"), uuid.MustParse("00000000-0000-0000-0000-000000000004")}
	for _, id := range ids {
		f.add(t, id)
	}
	orphan := uuid.New()
	f.add(t, orphan)
	_, err := fixturerepo.New(f.db).UnattributeMessage(t.Context(), orphan, project(f.project))
	require.NoError(t, err)
	p, err := f.store.Progress(t.Context(), f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Waiting: 3, Unattributed: 1}, p)
	first, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 2)
	require.NoError(t, err)
	require.Equal(t, Page{After: ids[0], Count: 2, More: true}, first)
	second, err := f.store.Backfill(t.Context(), f.org, f.project, first.After, 2)
	require.NoError(t, err)
	require.Equal(t, Page{After: ids[2], Count: 1, More: false}, second)
	p, err = f.store.Progress(t.Context(), f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Current: 3, Unattributed: 1}, p)
	lower := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	f.add(t, lower)
	empty, err := f.store.Backfill(t.Context(), f.org, f.project, second.After, 2)
	require.NoError(t, err)
	require.Zero(t, empty.Count)
	sweep, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 500)
	require.NoError(t, err)
	require.Equal(t, 1, sweep.Count)
	for _, limit := range []int32{0, -1, 501} {
		_, err = f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, limit)
		require.ErrorIs(t, err, ErrInvalid)
	}
	_, err = f.store.Load(t.Context(), f.org, f.project, orphan)
	require.ErrorIs(t, err, ErrNotFound)
}
func TestKeyFailureAndCallerRollback(t *testing.T) {
	t.Parallel()
	f := setup(t, false)
	id := uuid.New()
	f.add(t, id)
	page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 2)
	require.Error(t, err)
	require.Equal(t, Page{}, page)
	progress, err := f.store.Progress(t.Context(), f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, int64(1), progress.Waiting)
	require.NoError(t, f.keys.Provisioner.EnsureUseCase(t.Context(), f.org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	// A later missing ID aborts the caller's whole batch, including the first copy.
	low := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	f.add(t, low)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	require.ErrorIs(t, f.store.Capture(t.Context(), tx, f.org, f.project, []uuid.UUID{low, uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")}), ErrNotFound)
	require.NoError(t, tx.Rollback(t.Context()))
	_, err = f.store.Load(t.Context(), f.org, f.project, low)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, f.keys.Provisioner.Suspend(t.Context(), f.org, "example", orgencryption.SystemActor()))
	page, err = f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 2)
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	require.Equal(t, Page{}, page)
	require.NotContains(t, err.Error(), f.org)
}
func TestSemanticJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b  []byte
		equal bool
	}{
		{nil, []byte("null"), false}, {[]byte("null"), []byte("null"), true},
		{[]byte(`{"n":9007199254740993}`), []byte(`{"n":9007199254740992}`), false},
		{[]byte(`{"n":1e2,"a":[1.0]}`), []byte(`{"a":[1],"n":100.00}`), true},
		{[]byte(`{"a":null}`), []byte(`{"b":null}`), false},
	} {
		require.Equal(t, tc.equal, sameJSON(tc.a, tc.b))
	}
	// Redaction preserves errors.Is across database/provider errors.
	require.ErrorIs(t, safe(context.DeadlineExceeded), context.DeadlineExceeded)
	require.NoError(t, (&Store{}).Capture(context.Background(), nil, "", uuid.Nil, nil))
	_, err := New(nil, nil)
	require.ErrorIs(t, err, ErrInvalid)
}

// beforeCAS injects a committed concurrent edit between the snapshot and CAS.
type beforeCAS struct {
	repo.DBTX
	once func()
}

func (d *beforeCAS) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if d.once != nil {
		f := d.once
		d.once = nil
		f()
	}
	tag, err := d.DBTX.Exec(ctx, sql, args...) //nolint:glint // notestingrawsql: transparently intercept generated CAS execution to synchronize concurrent transactions.
	return tag, safe(err)
}
func TestCaptureConcurrentEditReturnsChanged(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	racing := &beforeCAS{DBTX: tx, once: func() {
		_, err := fixturerepo.New(f.db).RaceRawContent(t.Context(), id, project(f.project))
		require.NoError(t, err)
	}}
	require.ErrorIs(t, f.store.Capture(t.Context(), racing, f.org, f.project, []uuid.UUID{id}), ErrChanged)
	require.NoError(t, tx.Rollback(t.Context()))
	_, err = f.store.Load(t.Context(), f.org, f.project, id)
	require.ErrorIs(t, err, ErrNotFound)
}
func TestBackfillRollsBackEarlierSuccessfulCopies(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	f.add(t, first)
	f.add(t, second)
	// Fail the second insertion after the first has succeeded inside the page.
	_, err := f.db.Exec(t.Context(), `CREATE FUNCTION fail_second_transcript() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.message_id='00000000-0000-0000-0000-000000000002'::uuid THEN RAISE EXCEPTION 'example failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_second BEFORE INSERT ON chat_message_transcripts FOR EACH ROW EXECUTE FUNCTION fail_second_transcript()`) //nolint:glint // notestingrawsql: test-only trigger DDL injects a failure after an earlier page insertion.
	require.NoError(t, err)
	page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 2)
	require.Error(t, err)
	require.Equal(t, Page{}, page)
	require.NotContains(t, err.Error(), "example failure")
	progress, err := f.store.Progress(t.Context(), f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Waiting: 2}, progress)
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER fail_second ON chat_message_transcripts`) //nolint:glint // notestingrawsql: test-only trigger DDL injects a failure after an earlier page insertion.
	require.NoError(t, err)
	page, err = f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 2)
	require.NoError(t, err)
	require.Equal(t, 2, page.Count)
}
func TestUnattributedOwnershipFailsClosed(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	wrong := uuid.New()
	_, err := fixturerepo.New(f.db).CreateOtherProject(t.Context(), wrong, f.org)
	require.NoError(t, err)
	_, err = fixturerepo.New(f.db).MoveMessageProject(t.Context(), project(wrong), id, project(f.project))
	require.NoError(t, err)
	for _, pid := range []uuid.UUID{f.project, wrong} {
		require.ErrorIs(t, f.store.Capture(t.Context(), f.db, f.org, pid, []uuid.UUID{id}), ErrNotFound)
		_, err = f.store.Load(t.Context(), f.org, pid, id)
		require.ErrorIs(t, err, ErrNotFound)
	}
	progress, err := f.store.Progress(t.Context(), f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Unattributed: 1}, progress)
	page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 500)
	require.NoError(t, err)
	require.Zero(t, page.Count)
}

func TestNullnessFingerprintBackfill(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	for _, column := range []string{"content_raw", "tool_calls", "tool_call_summaries"} {
		// Identifiers are fixed test constants, not caller input.
		_, err := fixturerepo.New(f.db).SetJSONField(t.Context(), column, []byte(nil), id, project(f.project))
		require.NoError(t, err)
		f.capture(t, id)
		for _, value := range [][]byte{[]byte("null"), nil} {
			_, err = fixturerepo.New(f.db).SetJSONField(t.Context(), column, value, id, project(f.project))
			require.NoError(t, err)
			_, err = f.store.Load(t.Context(), f.org, f.project, id)
			require.ErrorIs(t, err, ErrStale)
			progress, err := f.store.Progress(t.Context(), f.org, f.project)
			require.NoError(t, err)
			require.Equal(t, Progress{Waiting: 1}, progress)
			page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 1)
			require.NoError(t, err)
			require.Equal(t, 1, page.Count)
			_, err = f.store.Load(t.Context(), f.org, f.project, id)
			require.NoError(t, err)
			progress, err = f.store.Progress(t.Context(), f.org, f.project)
			require.NoError(t, err)
			require.Equal(t, Progress{Current: 1}, progress)
		}
	}
}

// TestCaptureDoesNotInvertChatAndWrapLocks reproduces the compaction schedule:
// compaction owns the chat lock, while ordinary capture has already unwrapped
// its key and is about to take the CAS chat lock. Transactional key unwrap must
// not UPDATE last_unwrapped_at and hold the wrap lock across that CAS.
func TestCaptureDoesNotInvertChatAndWrapLocks(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	normalID, compactionID := uuid.New(), uuid.New()
	f.add(t, normalID)
	f.add(t, compactionID)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	compaction, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = compaction.Rollback(ctx) }()
	require.NoError(t, fixturerepo.New(compaction).LockChat(ctx, f.chat, f.project, f.org))
	normal, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = normal.Rollback(ctx) }()
	paused, release := make(chan struct{}), make(chan struct{})
	released := false
	// Fresh services ensure both captures unwrap cold keys through their own
	// caller transactions rather than observing a warmed process cache.
	cold := func() *Store {
		service, err := orgencryption.NewService(testenv.NewLogger(t), f.db, gcpauth.NewIdentity(gcpauth.NewStubResolver()), f.keys.KMS.Clients, f.keys.Keys)
		require.NoError(t, err)
		store, err := New(f.db, service)
		require.NoError(t, err)
		return store
	}
	ordinaryStore, compactionStore := cold(), cold()
	intercepted := &beforeCAS{DBTX: normal, once: func() {
		close(paused)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}}
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- ordinaryStore.Capture(ctx, intercepted, f.org, f.project, []uuid.UUID{normalID})
	}()
	defer func() {
		if !released {
			close(release)
		}
		cancel()
		<-done // Join before rolling back the caller transaction on a failure path.
	}()
	select {
	case <-paused:
	case <-ctx.Done():
		t.Fatal("ordinary capture did not reach CAS before deadline")
	}
	// This must finish before releasing the ordinary CAS. With an unwrap UPDATE,
	// it waits on ordinary capture's wrap lock and cannot commit its chat lock.
	compactionCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	require.NoError(t, compactionStore.Capture(compactionCtx, compaction, f.org, f.project, []uuid.UUID{compactionID}))
	require.NoError(t, compaction.Commit(ctx))
	close(release)
	released = true
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("ordinary capture did not finish after compaction commit")
	}
	require.NoError(t, normal.Commit(ctx))
	for _, id := range []uuid.UUID{normalID, compactionID} {
		_, err = f.store.Load(ctx, f.org, f.project, id)
		require.NoError(t, err)
	}
}

func TestStorageErrorFingerprintBackfill(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)
	for _, value := range []pgtype.Text{{String: "sensitive provider error", Valid: true}, {String: "", Valid: true}, {}} {
		n, err := fixturerepo.New(f.db).SetStorageError(t.Context(), value, id, project(f.project))
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
		opened, err := f.store.Load(t.Context(), f.org, f.project, id)
		require.ErrorIs(t, err, ErrStale)
		require.Equal(t, transcriptcrypto.Message{}, opened)
		progress, err := f.store.Progress(t.Context(), f.org, f.project)
		require.NoError(t, err)
		require.Equal(t, Progress{Waiting: 1}, progress)
		page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 1)
		require.NoError(t, err)
		require.Equal(t, 1, page.Count)
		progress, err = f.store.Progress(t.Context(), f.org, f.project)
		require.NoError(t, err)
		require.Equal(t, Progress{Current: 1}, progress)
		opened, err = f.store.Load(t.Context(), f.org, f.project, id)
		require.NoError(t, err)
		if value.Valid {
			require.NotNil(t, opened.StorageError)
			require.Equal(t, value.String, *opened.StorageError)
		} else {
			require.Nil(t, opened.StorageError)
		}
		ciphertext, err := fixturerepo.New(f.db).Ciphertext(t.Context(), id, f.project, f.org)
		require.NoError(t, err)
		require.NotContains(t, ciphertext, "sensitive provider error")
	}
	// A forged public fingerprint cannot authenticate changed error text.
	_, err := fixturerepo.New(f.db).SetStorageError(t.Context(), pgtype.Text{String: "sensitive forged error", Valid: true}, id, project(f.project))
	require.NoError(t, err)
	_, err = fixturerepo.New(f.db).ForgeSourceHash(t.Context(), id, project(f.project), f.org)
	require.NoError(t, err)
	opened, err := f.store.Load(t.Context(), f.org, f.project, id)
	require.ErrorIs(t, err, ErrStale)
	require.Equal(t, transcriptcrypto.Message{}, opened)
}
