package lake_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/infra/pkg/storage"
	"github.com/speakeasy-api/gram/server/internal/lake"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestFilesystemStorePublishesCompleteObjectWithoutOverwrite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	directory := t.TempDir()
	store, err := lake.NewFilesystemStore(ctx, testenv.NewLogger(t), directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	object := storage.Object{Bucket: "lake", Name: "readings/part__year=2026/example.parquet"}
	final := filepath.Join(directory, object.Bucket, object.Name)

	err = store.Write(ctx, object, func(w io.Writer) error {
		_, statErr := os.Stat(final)
		require.ErrorIs(t, statErr, os.ErrNotExist)
		_, writeErr := io.WriteString(w, "complete object")
		require.NoError(t, writeErr)
		return nil
	})
	require.NoError(t, err)

	err = store.Write(ctx, object, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, "replacement")
		require.NoError(t, writeErr)
		return nil
	})
	require.ErrorIs(t, err, os.ErrExist)
	contents, err := os.ReadFile(final)
	require.NoError(t, err)
	require.Equal(t, "complete object", string(contents))
	entries, err := os.ReadDir(filepath.Dir(final))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestFilesystemStoreCreatesNestedRootThroughSymlink(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	require.NoError(t, os.Mkdir(physical, 0o750))
	alias := filepath.Join(base, "alias")
	require.NoError(t, os.Symlink(physical, alias))
	store, err := lake.NewFilesystemStore(t.Context(), testenv.NewLogger(t), filepath.Join(alias, "new-parent", "lake-root"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	err = store.Write(t.Context(), storage.Object{Bucket: "lake", Name: "example.parquet"}, func(w io.Writer) error {
		_, err := io.WriteString(w, "complete object")
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, err)
	contents, err := os.ReadFile(filepath.Join(physical, "new-parent", "lake-root", "lake", "example.parquet"))
	require.NoError(t, err)
	require.Equal(t, "complete object", string(contents))
}

func TestFilesystemStoreAbortsIncompleteObjects(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"error", "panic", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			directory := t.TempDir()
			store, err := lake.NewFilesystemStore(ctx, testenv.NewLogger(t), directory)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			object := storage.Object{Bucket: "lake", Name: "example.parquet"}
			failure := errors.New("encoding failed")
			encode := func(w io.Writer) error {
				_, err := io.WriteString(w, "incomplete")
				require.NoError(t, err)
				switch mode {
				case "panic":
					panic(failure)
				case "cancellation":
					cancel()
					return nil
				default:
					return failure
				}
			}

			if mode == "panic" {
				require.PanicsWithValue(t, failure, func() { _ = store.Write(ctx, object, encode) })
			} else {
				err = store.Write(ctx, object, encode)
				if mode == "cancellation" {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.ErrorIs(t, err, failure)
				}
			}

			entries, err := os.ReadDir(filepath.Join(directory, "lake"))
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestFilesystemStoreRejectsEscapingPaths(t *testing.T) {
	t.Parallel()
	for _, object := range []storage.Object{
		{Bucket: "../outside", Name: "example.parquet"},
		{Bucket: "lake", Name: "../example.parquet"},
		{Bucket: "lake", Name: "/example.parquet"},
	} {
		t.Run(object.Bucket+"/"+object.Name, func(t *testing.T) {
			t.Parallel()
			store, err := lake.NewFilesystemStore(t.Context(), testenv.NewLogger(t), t.TempDir())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			err = store.Write(t.Context(), object, func(io.Writer) error {
				t.Fatal("encoder should not run")
				return nil
			})
			require.Error(t, err)
		})
	}
}

func TestFilesystemStoreRejectsEscapingSymlink(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(directory, "lake")))
	store, err := lake.NewFilesystemStore(t.Context(), testenv.NewLogger(t), directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	err = store.Write(t.Context(), storage.Object{Bucket: "lake", Name: "example.parquet"}, func(io.Writer) error {
		t.Fatal("encoder should not run")
		return nil
	})
	require.Error(t, err)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestFilesystemStoreConcurrentWritersPublishOnlyOneObject(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := lake.NewFilesystemStore(t.Context(), testenv.NewLogger(t), directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	object := storage.Object{Bucket: "lake", Name: "example.parquet"}
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			results <- store.Write(t.Context(), object, func(w io.Writer) error {
				ready.Done()
				ready.Wait()
				_, err := io.WriteString(w, "complete object")
				if err != nil {
					return fmt.Errorf("write concurrent object: %w", err)
				}
				return nil
			})
		}()
	}

	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, os.ErrExist)
	} else {
		require.ErrorIs(t, first, os.ErrExist)
		require.NoError(t, second)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "lake"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
