package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type FSBlobStore struct {
	mut    sync.Mutex
	Root   *os.Root
	Logger *slog.Logger
}

func NewFSBlobStore(logger *slog.Logger, root *os.Root) *FSBlobStore {
	return &FSBlobStore{
		mut:    sync.Mutex{},
		Root:   root,
		Logger: logger.With(attr.SlogComponent("blob-store-fs")),
	}
}

var _ BlobStore = (*FSBlobStore)(nil)

func (fbs *FSBlobStore) getPath(u *url.URL) (string, error) {
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	p := strings.TrimPrefix(u.String(), "file://")
	return filepath.Join(strings.Split(p, "/")...), nil
}

func (fbs *FSBlobStore) Exists(ctx context.Context, u *url.URL) (bool, error) {
	filepath, err := fbs.getPath(u)
	if err != nil {
		return false, fmt.Errorf("generate asset path: %w", err)
	}

	fbs.mut.Lock()
	defer fbs.mut.Unlock()

	stat, err := fbs.Root.Stat(filepath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("stat file: %w", err)
	default:
		return stat.Mode().IsRegular(), nil
	}
}
func (fbs *FSBlobStore) Read(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	filepath, err := fbs.getPath(u)
	if err != nil {
		return nil, fmt.Errorf("generate asset path: %w", err)
	}

	fbs.mut.Lock()
	defer fbs.mut.Unlock()

	f, err := fbs.Root.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("open file for reading: %w", err)
	}

	return f, nil
}

func (fbs *FSBlobStore) ReadAt(ctx context.Context, u *url.URL) (ReaderAtCloser, int64, error) {
	filepath, err := fbs.getPath(u)
	if err != nil {
		return nil, 0, fmt.Errorf("generate asset path: %w", err)
	}

	fbs.mut.Lock()
	defer fbs.mut.Unlock()

	f, err := fbs.Root.Open(filepath)
	if err != nil {
		return nil, 0, fmt.Errorf("open file for reading: %w", err)
	}

	stat, err := f.Stat()
	if err != nil {
		defer o11y.LogDefer(ctx, fbs.Logger, func() error {
			return f.Close()
		})
		return nil, 0, fmt.Errorf("stat file: %w", err)
	}

	return f, stat.Size(), nil
}

func (fbs *FSBlobStore) Write(ctx context.Context, pathname string, contentType string, contentLength int64) (io.WriteCloser, *url.URL, error) {
	fbs.mut.Lock()
	defer fbs.mut.Unlock()

	fspath := filepath.Join(strings.Split(pathname, "/")...)
	if err := fbs.mkdirAll(fspath); err != nil {
		return nil, nil, err
	}

	dst, err := fbs.Root.OpenFile(fspath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("open file for writing: %w", err)
	}

	return dst, &url.URL{Scheme: "file", Path: fspath}, nil
}

func (fbs *FSBlobStore) mkdirAll(filename string) error {
	dir := filepath.Dir(filepath.Clean(filename))

	segments := []string{}
	limit := 10 // hard limit to prevent infinite loops
	for i := range limit {
		if dir == "" || dir == "." || dir == "/" {
			break
		}

		segments = append(segments, dir)
		next, _ := filepath.Split(dir)
		if next == dir {
			break
		}
		dir = next

		if i == limit-1 {
			return fmt.Errorf("too many segments: %s", filename)
		}
	}

	for _, seg := range slices.Backward(segments) {
		if err := fbs.Root.Mkdir(seg, 0755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create directory %s: %w", seg, err)
		}
	}

	return nil
}

func (fbs *FSBlobStore) PresignRead(ctx context.Context, subpath string, ttl time.Duration) (*url.URL, error) {
	fspath := filepath.Join(strings.Split(subpath, "/")...)
	return &url.URL{Scheme: "file", Path: fspath}, nil
}
