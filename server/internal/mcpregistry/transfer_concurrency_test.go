package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
)

// Hold a normal writer at the advisory-lock/INSERT boundary, then start an
// import. Waiting for its advisory lock makes the interleaving deterministic.
func TestCatalogImportConcurrentCreate(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: hold a real caller-owned transaction across concurrent operations to test PostgreSQL lock ordering.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	require.NoError(t, repo.New(tx).LockOktaMappings(ctx))
	imported := make(chan error, 1)
	go func() {
		_, err := NewCatalogImporter(s, "local", nil).Import(ctx, CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(oktaRecord("example.test/imported", "imported"))}})
		imported <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database()))`).Scan(&waiting) //nolint:glint // notestingrawsql: observe PostgreSQL lock waits to schedule the regression; not a production registry query.
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	_, err = s.CreateTx(ctx, tx, json.RawMessage(oktaRecord("example.test/concurrent", "concurrent")))
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	require.NoError(t, err, "normal writer must not deadlock with import")
	require.NoError(t, <-imported, "import must not deadlock with normal writer")
	_, err = s.GetByName(ctx, "example.test/imported")
	require.NoError(t, err)
	_, err = s.GetByName(ctx, "example.test/concurrent")
	require.NoError(t, err)
}

// Uploading is external I/O: it must not block catalog mutations. A concurrent
// create during upload also exercises the authoritative name check afterwards.
func TestCatalogImportUploadRevalidatesName(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	const src = "https://source.example.test/logo.svg"
	raw, err := WithCatalogIcons(json.RawMessage(basicRecord), CatalogIcons{Default: &CatalogIcon{Src: src, MimeType: "image/svg+xml", Sizes: []string{"any"}}})
	require.NoError(t, err)
	calls := 0
	writer := catalogWriterFunc(func(ctx context.Context, _ []byte) (string, error) {
		calls++
		_, err := s.Create(ctx, json.RawMessage(basicRecord))
		return "https://destination.example.test/logo.svg", err
	})
	importer := NewCatalogImporter(s, "local", writer)
	bundle := CatalogBundle{Version: 1, Entries: []json.RawMessage{raw}, Assets: map[string][]byte{src: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)}}
	result, err := importer.Import(ctx, bundle)
	require.NoError(t, err)
	require.Empty(t, result.Created)
	require.Equal(t, []string{"example.test/demo"}, result.Conflicts)
	got, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	icons, err := ParseCatalogIcons(got.Data)
	require.NoError(t, err)
	require.Nil(t, icons.Default, "concurrent creator must not be overwritten")
	result, err = importer.Import(ctx, bundle)
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/demo"}, result.Conflicts)
	require.Equal(t, 1, calls, "repeat import should not upload existing entries' logos")
}

type catalogWriterFunc func(context.Context, []byte) (string, error)

func (f catalogWriterFunc) StoreCatalogAsset(ctx context.Context, data []byte) (string, error) {
	return f(ctx, data)
}

// A row-locking writer already owns ROW SHARE before claiming the mapping
// advisory lock. Import must neither reverse that order nor prevent its upgrade
// to ROW EXCLUSIVE for the final UPDATE.
func TestCatalogImportConcurrentMutation(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"save", "publish", "icons"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			ctx, s, db := newTestService(t)
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			original, err := s.Create(ctx, json.RawMessage(oktaRecord("example.test/edited", "edited")))
			require.NoError(t, err)
			if operation == "publish" {
				original, err = s.SetPublished(ctx, original.ID, Token(original), false)
				require.NoError(t, err)
			}
			tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: hold a real caller-owned transaction across concurrent operations to test PostgreSQL lock ordering.
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(context.Background()) }()
			_, err = repo.New(tx).LockEntry(ctx, original.ID)
			require.NoError(t, err)
			require.NoError(t, repo.New(tx).LockOktaMappings(ctx))
			imported := make(chan error, 1)
			go func() {
				_, err := NewCatalogImporter(s, "local", nil).Import(ctx, CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(oktaRecord("example.test/imported", "imported"))}})
				imported <- err
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database()))`).Scan(&waiting) //nolint:glint // notestingrawsql: observe PostgreSQL lock waits to schedule the regression; not a production registry query.
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)
			switch operation {
			case "save":
				_, err = s.mutateTx(ctx, tx, original.ID, Token(original), original.Data, nil)
			case "publish":
				published := true
				_, err = s.mutateTx(ctx, tx, original.ID, Token(original), nil, &published)
			case "icons":
				_, err = s.SetCatalogIconsTx(ctx, tx, original.ID, Token(original), CatalogIcons{})
			}
			if err == nil {
				err = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
			require.NoError(t, err)
			require.NoError(t, <-imported)
			got, err := s.Get(ctx, original.ID)
			require.NoError(t, err)
			require.True(t, got.Published)
			require.NotEqual(t, Token(original), Token(got))
		})
	}
}

// CreateTx is composable: an earlier unmapped insert can already hold ROW
// EXCLUSIVE when a later operation needs the mapping advisory lock.
func TestCatalogImportConcurrentMultiOperationTransaction(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: hold a real caller-owned transaction across concurrent operations to test PostgreSQL lock ordering.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = s.CreateTx(ctx, tx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	// The tracer only schedules the interleaving; all locks and writes are real
	// PostgreSQL operations. At this point import owns the advisory lock.
	trace := &catalogTableLockTrace{started: make(chan struct{})}
	cfg := db.Config()
	cfg.ConnConfig.Tracer = trace
	importDB, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer importDB.Close()
	imported := make(chan error, 1)
	go func() {
		_, err := NewCatalogImporter(New(importDB, s.validator), "local", nil).Import(ctx, CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(oktaRecord("example.test/imported", "imported"))}})
		imported <- err
	}()
	select {
	case <-trace.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err = s.CreateTx(ctx, tx, json.RawMessage(oktaRecord("example.test/second", "second")))
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	require.NoError(t, err, "composed writer must not deadlock with import")
	require.NoError(t, <-imported, "import must not deadlock with composed writer")
}

type catalogTableLockTrace struct {
	started chan struct{}
	once    sync.Once
}

func (t *catalogTableLockTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "LOCK TABLE mcp_registry_entries") {
		t.once.Do(func() { close(t.started) })
	}
	return ctx
}
func (*catalogTableLockTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Cancellation after a real NOWAIT failure must release the advisory lock and
// return the pool connection, not leave the next import stuck behind either.
func TestCatalogImportCancelLockRetry(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: retain ROW EXCLUSIVE across import retries to exercise real PostgreSQL contention.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = s.CreateTx(ctx, tx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	trace := &catalogRetryTrace{retried: make(chan struct{})}
	cfg := db.Config()
	cfg.MaxConns = 1
	cfg.ConnConfig.Tracer = trace
	importDB, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer importDB.Close()
	importer := NewCatalogImporter(New(importDB, s.validator), "local", nil)
	bundle := CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(oktaRecord("example.test/imported", "imported"))}}
	importCtx, cancelImport := context.WithCancel(ctx)
	defer cancelImport()
	imported := make(chan error, 1)
	go func() {
		_, err := importer.Import(importCtx, bundle)
		imported <- err
	}()
	select {
	case <-trace.retried:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancelImport()
	require.ErrorIs(t, <-imported, context.Canceled)
	require.NoError(t, tx.Rollback(ctx))
	// With a one-connection pool, this checks both connection reuse and successful
	// reacquisition of the advisory/table locks through the production interface.
	result, err := importer.Import(ctx, bundle)
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/imported"}, result.Created)
	require.NoError(t, importDB.Ping(ctx))
}

type catalogRetryTrace struct {
	retried chan struct{}
	once    sync.Once
}

func (*catalogRetryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (t *catalogRetryTrace) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var pgerr *pgconn.PgError
	if errors.As(data.Err, &pgerr) && pgerr.Code == pgerrcode.LockNotAvailable {
		t.once.Do(func() { close(t.retried) })
	}
}
