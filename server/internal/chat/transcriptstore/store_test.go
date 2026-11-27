package transcriptstore

import (
	"context"
	"log"
	"os"
	"strings"
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
	f.store, err = New(db, f.keys.Service, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	if provision {
		require.NoError(t, f.keys.Provisioner.EnsureUseCase(t.Context(), f.org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
		// Writers warm the key before their transaction; Capture never calls KMS.
		require.NoError(t, f.store.Prepare(t.Context(), f.org))
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

// Capture never calls KMS inside the caller's transaction: a cold key fails
// the capture, and Prepare on the pool before the transaction warms it.
func TestCaptureWithOneConnectionNeedsAWarmKey(t *testing.T) {
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
	store, err := New(pool, service, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	before := f.keys.KMS.Decrypts()
	cold, err := pool.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = cold.Rollback(context.Background()) }()
	require.ErrorIs(t, store.Capture(ctx, cold, f.org, f.project, []uuid.UUID{first}), orgencryption.ErrKeyNotWarm)
	require.NoError(t, cold.Rollback(ctx))
	require.Equal(t, before, f.keys.KMS.Decrypts(), "no KMS call inside the transaction")

	require.NoError(t, store.Prepare(ctx, f.org))
	require.Equal(t, before+1, f.keys.KMS.Decrypts())
	tx, err := pool.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	require.NoError(t, store.Capture(ctx, tx, f.org, f.project, []uuid.UUID{first, second}))
	require.Equal(t, before+1, f.keys.KMS.Decrypts(), "the batch uses the warm key")
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
	require.Equal(t, transcriptcrypto.ProtectedFields{}, opened)
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

// Capture's message lock lasts until the caller commits, so no body update can
// land between the snapshot it seals and the commit.
func TestCaptureLocksTheMessageUntilCommit(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
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
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "55P03", pgErr.Code)
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
	require.Equal(t, 4, sweep.Count)
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
	require.NoError(t, f.store.Prepare(t.Context(), f.org))
	// A later missing ID aborts the caller's whole batch, including the first copy.
	low := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	f.add(t, low)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises atomicity and lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
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
	_, err := New(nil, nil, nil, nil, nil)
	require.ErrorIs(t, err, ErrInvalid)
}

// afterCaptureExec runs a hook once, right after the Capture upsert and
// before the metadata update that follows it in the same transaction.
type afterCaptureExec struct {
	repo.DBTX
	once func()
}

func (d *afterCaptureExec) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := d.DBTX.Exec(ctx, sql, args...) //nolint:glint // notestingrawsql: transparently intercept generated capture execution to interleave two transactions.
	if d.once != nil && strings.Contains(sql, "-- name: Capture ") {
		f := d.once
		d.once = nil
		f()
	}
	return tag, safe(err)
}

// Two captures of one message must not deadlock: each one updates the message
// after its upsert, so the upsert takes a lock that makes the second wait.
func TestConcurrentCapturesOfOneMessageDoNotDeadlock(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ctx := t.Context()
	id := uuid.New()
	f.add(t, id)
	first, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = first.Rollback(ctx) }()
	second, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction boundary exercises lock lifetime; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = second.Rollback(ctx) }()

	secondDone := make(chan error, 1)
	hook := &afterCaptureExec{DBTX: first, once: func() {
		go func() { secondDone <- f.store.Capture(ctx, second, f.org, f.project, []uuid.UUID{id}) }()
		// Let the second capture take every lock it can get, until it waits
		// on one, before the first goes on to update the message.
		require.Eventually(t, func() bool {
			var waiting int
			err := f.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting) //nolint:glint // notestingrawsql: observes lock waits to order two transactions deterministically.
			return err == nil && waiting > 0
		}, 5*time.Second, 10*time.Millisecond)
	}}
	require.NoError(t, f.store.Capture(ctx, hook, f.org, f.project, []uuid.UUID{id}))
	require.NoError(t, first.Commit(ctx))
	require.NoError(t, <-secondDone)
	require.NoError(t, second.Commit(ctx))
}

// A transcript sealed before body updates resealed it still carries source_hash
// and may be stale, so it reads as missing until backfill reseals it, which
// clears the hash.
func TestLegacySourceHashTranscripts(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ctx := t.Context()
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)
	fixtures := fixturerepo.New(f.db)
	n, err := fixtures.MakeLegacyCopy(ctx, id, project(f.project))
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	_, err = fixtures.ChangeContent(ctx, id, project(f.project))
	require.NoError(t, err)
	_, err = f.store.Load(ctx, f.org, f.project, id)
	require.ErrorIs(t, err, ErrNotFound, "a legacy transcript reads as missing")
	many, err := f.store.LoadMany(ctx, f.org, f.project, []uuid.UUID{id})
	require.NoError(t, err)
	require.Empty(t, many, "so the reader serves plaintext")
	progress, err := f.store.Progress(ctx, f.org, f.project)
	require.NoError(t, err)
	require.Equal(t, Progress{Waiting: 1}, progress)

	page, err := f.store.Backfill(ctx, f.org, f.project, uuid.Nil, 500)
	require.NoError(t, err)
	require.Equal(t, 1, page.Count)
	opened, err := f.store.Load(ctx, f.org, f.project, id)
	require.NoError(t, err)
	require.Equal(t, "changed", opened.Content)
	hashed, err := fixtures.CopySourceHashIsSet(ctx, id)
	require.NoError(t, err)
	require.False(t, hashed, "resealing clears the legacy hash")
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
			// A write that bypasses the application leaves the transcript in
			// place, and the read catches it.
			_, err = f.store.Load(t.Context(), f.org, f.project, id)
			require.ErrorIs(t, err, ErrStale)
			page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 1)
			require.NoError(t, err)
			require.Equal(t, 1, page.Count)
			_, err = f.store.Load(t.Context(), f.org, f.project, id)
			require.NoError(t, err)
			progress, err := f.store.Progress(t.Context(), f.org, f.project)
			require.NoError(t, err)
			require.Equal(t, Progress{Current: 1}, progress)
		}
	}
}

// TestCaptureDoesNotInvertChatAndWrapLocks reproduces the compaction schedule:
// compaction owns the chat lock, while ordinary capture waits for it at its
// snapshot. Compaction must still unwrap its key and finish: ordinary capture
// holds no wrap lock while it waits.
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
	// Separate services, each warmed on the pool as writers do before their
	// transaction, so neither capture touches a key row inside it.
	warm := func() *Store {
		service, err := orgencryption.NewService(testenv.NewLogger(t), f.db, gcpauth.NewIdentity(gcpauth.NewStubResolver()), f.keys.KMS.Clients, f.keys.Keys)
		require.NoError(t, err)
		store, err := New(f.db, service, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
		require.NoError(t, err)
		require.NoError(t, store.Prepare(ctx, f.org))
		return store
	}
	ordinaryStore, compactionStore := warm(), warm()
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- ordinaryStore.Capture(ctx, normal, f.org, f.project, []uuid.UUID{normalID})
	}()
	defer func() {
		cancel()
		<-done // Join before rolling back the caller transaction on a failure path.
	}()
	require.Eventually(t, func() bool {
		var waiting int
		err := f.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting) //nolint:glint // notestingrawsql: observes lock waits to order two transactions deterministically.
		return err == nil && waiting > 0
	}, 5*time.Second, 10*time.Millisecond, "ordinary capture waits for the chat lock")
	// This must finish while ordinary capture waits. If ordinary capture held a
	// wrap lock, compaction's unwrap would wait on it and never commit.
	compactionCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	require.NoError(t, compactionStore.Capture(compactionCtx, compaction, f.org, f.project, []uuid.UUID{compactionID}))
	require.NoError(t, compaction.Commit(ctx))
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
		require.Equal(t, transcriptcrypto.ProtectedFields{}, opened)
		page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 1)
		require.NoError(t, err)
		require.Equal(t, 1, page.Count)
		progress, err := f.store.Progress(t.Context(), f.org, f.project)
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
	// Error text changed by a write that bypassed the application reads as stale.
	_, err := fixturerepo.New(f.db).SetStorageError(t.Context(), pgtype.Text{String: "sensitive forged error", Valid: true}, id, project(f.project))
	require.NoError(t, err)
	opened, err := f.store.Load(t.Context(), f.org, f.project, id)
	require.ErrorIs(t, err, ErrStale)
	require.Equal(t, transcriptcrypto.ProtectedFields{}, opened)
	page, err := f.store.Backfill(t.Context(), f.org, f.project, uuid.Nil, 1)
	require.NoError(t, err)
	require.Equal(t, 1, page.Count)
	opened, err = f.store.Load(t.Context(), f.org, f.project, id)
	require.NoError(t, err)
	require.NotNil(t, opened.StorageError)
	require.Equal(t, "sensitive forged error", *opened.StorageError)
}

func TestTranscriptOwnershipForeignKeys(t *testing.T) {
	t.Parallel()
	f := setup(t, false)
	id := uuid.New()
	f.add(t, id)
	otherProject := uuid.New()
	q := fixturerepo.New(f.db)
	_, err := q.CreateOtherProject(t.Context(), otherProject, f.org)
	require.NoError(t, err)
	otherOrg := "org_" + uuid.NewString()
	_, err = q.CreateOrganization(t.Context(), otherOrg)
	require.NoError(t, err)
	for _, target := range []struct {
		projectID      uuid.UUID
		organizationID string
		constraint     string
	}{
		{otherProject, f.org, "chat_message_transcripts_project_id_message_id_fkey"},
		{f.project, otherOrg, "chat_message_transcripts_organization_id_project_id_fkey"},
	} {
		_, err := q.InsertTranscript(t.Context(), id, target.projectID, target.organizationID)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "23503", pgErr.Code)
		require.Equal(t, target.constraint, pgErr.ConstraintName)
	}
	n, err := q.InsertTranscript(t.Context(), id, f.project, f.org)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}
