// Package lake provides local analytical object storage.
package lake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/infra/pkg/storage"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// FilesystemStore writes immutable objects beneath a local directory, with each
// bucket represented by a subdirectory. Parquet files retain their schema metadata.
type FilesystemStore struct {
	// root confines all object operations to the configured directory.
	root *os.Root

	// logger records failures while cleaning up incomplete writes.
	logger *slog.Logger
}

var _ storage.Store = (*FilesystemStore)(nil)

// NewFilesystemStore creates and opens the local storage directory.
func NewFilesystemStore(ctx context.Context, logger *slog.Logger, directory string) (*FilesystemStore, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("initialize filesystem lake: %w", err)
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf("create lake directory: %w", err)
	}

	// Persist the root's own directory entry as well as any ancestors MkdirAll
	// created. Object writes can only sync directories beneath the open root.
	if err := syncRootAncestors(ctx, logger, directory); err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open lake directory: %w", err)
	}

	return &FilesystemStore{root: root, logger: logger}, nil
}

func syncRootAncestors(ctx context.Context, logger *slog.Logger, directory string) error {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("resolve lake directory: %w", err)
	}

	// Follow the physical path so a symlinked local directory also persists the
	// ancestors containing the actual files, rather than just the symlink's path.
	current, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return fmt.Errorf("resolve physical lake directory: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sync lake root ancestors: %w", err)
		}

		dir, err := os.Open(current)
		if err != nil {
			return fmt.Errorf("open lake root ancestor: %w", err)
		}
		defer o11y.LogDefer(ctx, logger, "close lake root ancestor", dir.Close)

		if err := dir.Sync(); err != nil {
			return fmt.Errorf("sync lake root ancestor: %w", err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

// Close releases the directory handle after all storage runners have stopped.
func (s *FilesystemStore) Close() error {
	if err := s.root.Close(); err != nil {
		return fmt.Errorf("close filesystem lake: %w", err)
	}

	return nil
}

// Write publishes a fully synced object without replacing an existing object.
// Failed or panicking encoders leave no final object; incomplete temporary files
// are removed before returning. Bucket and object names must be relative paths.
func (s *FilesystemStore) Write(ctx context.Context, object storage.Object, encode func(io.Writer) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write lake object: %w", err)
	}
	if !fs.ValidPath(object.Bucket) || object.Bucket == "." || strings.Contains(object.Bucket, "/") ||
		!fs.ValidPath(object.Name) || object.Name == "." {
		return errors.New("invalid lake bucket or object name")
	}

	name := path.Join(object.Bucket, object.Name)
	directory := path.Dir(name)
	if err := s.root.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create lake object directory: %w", err)
	}

	temporary := path.Join(directory, ".parquet-"+uuid.NewString())
	f, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary lake object: %w", err)
	}
	defer o11y.LogDefer(ctx, s.logger, "remove temporary lake object", func() error { return s.root.Remove(temporary) })
	defer o11y.NoLogDefer(f.Close)

	if err := encode(f); err != nil {
		return fmt.Errorf("encode lake object: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("encode lake object: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync lake object: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close lake object: %w", err)
	}

	if err := s.root.Link(temporary, name); err != nil {
		return fmt.Errorf("publish lake object: %w", err)
	}

	// Sync every ancestor so newly created bucket and partition directories are
	// durable along with the final object link before the runner acknowledges it.
	for current := directory; ; current = path.Dir(current) {
		if err := s.syncDirectory(ctx, current); err != nil {
			removeErr := s.root.Remove(name)
			return errors.Join(err, removeErr)
		}
		if current == "." {
			break
		}
	}

	return nil
}

func (s *FilesystemStore) syncDirectory(ctx context.Context, directory string) error {
	f, err := s.root.Open(directory)
	if err != nil {
		return fmt.Errorf("open lake directory for sync: %w", err)
	}
	defer o11y.LogDefer(ctx, s.logger, "close lake directory", f.Close)

	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync lake directory: %w", err)
	}

	return nil
}
