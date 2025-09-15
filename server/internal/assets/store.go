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

	"cloud.google.com/go/storage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type ReaderAtCloser interface {
	io.ReaderAt
	io.Closer
}

type BlobStore interface {
	Exists(ctx context.Context, objectURL *url.URL) (bool, error)
	Read(ctx context.Context, objectURL *url.URL) (rdr io.ReadCloser, err error)
	ReadAt(ctx context.Context, objectURL *url.URL) (rdr ReaderAtCloser, size int64, err error)
	Write(ctx context.Context, urlpath string, src io.Reader, contentType string) (io.WriteCloser, *url.URL, error)
}

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

func (fbs *FSBlobStore) Write(ctx context.Context, pathname string, src io.Reader, contentType string) (io.WriteCloser, *url.URL, error) {
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

type GCSBlobStore struct {
	logger    *slog.Logger
	client    *storage.Client
	bucket    *storage.BucketHandle
	bucketURI *url.URL
}

var _ BlobStore = (*GCSBlobStore)(nil)

func NewGCSBlobStore(ctx context.Context, logger *slog.Logger, bucketURI string) (*GCSBlobStore, error) {
	client, err := storage.NewClient(ctx, storage.WithJSONReads())
	if err != nil {
		return nil, fmt.Errorf("create gcs client: %w", err)
	}

	uri, err := url.Parse(bucketURI)
	if err != nil {
		return nil, fmt.Errorf("parse bucket uri: %w", err)
	}

	bucket := client.Bucket(uri.Hostname())

	return &GCSBlobStore{
		logger:    logger.With(attr.SlogComponent("blob-store-gcs")),
		client:    client,
		bucket:    bucket,
		bucketURI: uri,
	}, nil
}

func (gbs *GCSBlobStore) getPath(u *url.URL) (string, error) {
	if u.Scheme != "gs" {
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	bu := gbs.bucketURI.String()
	if !strings.HasSuffix(bu, "/") {
		bu += "/"
	}

	if !strings.HasPrefix(u.String(), bu) {
		return "", fmt.Errorf("unauthorized access")
	}

	return strings.TrimPrefix(u.Path, "/"), nil
}

func (gbs *GCSBlobStore) getBucketURI(subpath string) (*url.URL, error) {
	noPrefix := strings.TrimPrefix(subpath, gbs.bucketURI.Path)
	noSlash := strings.TrimPrefix(noPrefix, "/")
	u := gbs.bucketURI.JoinPath(noSlash)
	us := u.String()
	if us == "" || !strings.HasPrefix(us, gbs.bucketURI.String()) || us == gbs.bucketURI.String() {
		return nil, fmt.Errorf("invalid path: %s", subpath)
	}

	return u, nil
}

func (gbs *GCSBlobStore) Exists(ctx context.Context, u *url.URL) (bool, error) {
	subpath, err := gbs.getPath(u)
	if err != nil {
		return false, fmt.Errorf("generate asset path: %w", err)
	}

	obj := gbs.bucket.Object(subpath)
	_, err = obj.Attrs(ctx)
	switch {
	case errors.Is(err, storage.ErrObjectNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("get object attrs: %w", err)
	default:
		return true, nil
	}
}

func (gbs *GCSBlobStore) Read(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	subpath, err := gbs.getPath(u)
	if err != nil {
		return nil, fmt.Errorf("generate asset path: %w", err)
	}

	rdr, err := gbs.bucket.Object(subpath).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}

	return rdr, nil
}

func (gbs *GCSBlobStore) ReadAt(ctx context.Context, u *url.URL) (ReaderAtCloser, int64, error) {
	subpath, err := gbs.getPath(u)
	if err != nil {
		return nil, 0, fmt.Errorf("generate asset path: %w", err)
	}

	obj := gbs.bucket.Object(subpath)
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("get object attrs: %w", err)
	}

	return &gcsChunkReader{
		logger:     gbs.logger,
		bucket:     gbs.bucket,
		objectPath: subpath,
		context:    func() context.Context { return ctx },
	}, attrs.Size, nil
}

func (gbs *GCSBlobStore) Write(ctx context.Context, subpath string, src io.Reader, contentType string) (io.WriteCloser, *url.URL, error) {
	uri, err := gbs.getBucketURI(subpath)
	if err != nil {
		return nil, nil, fmt.Errorf("generate asset path: %w", err)
	}

	w := gbs.bucket.Object(strings.TrimPrefix(uri.Path, "/")).NewWriter(ctx)
	w.ContentType = contentType

	return w, uri, nil
}

type gcsChunkReader struct {
	logger     *slog.Logger
	bucket     *storage.BucketHandle
	objectPath string
	context    func() context.Context
}

func (g *gcsChunkReader) ReadAt(p []byte, offset int64) (int, error) {
	ctx := g.context()
	rdr, err := g.bucket.Object(g.objectPath).NewRangeReader(ctx, offset, int64(len(p)))
	if err != nil {
		return 0, fmt.Errorf("create range reader: %w", err)
	}
	defer o11y.LogDefer(ctx, g.logger, func() error {
		return rdr.Close()
	})

	n, err := io.ReadFull(rdr, p)
	if err != nil {
		// Handle io.ReaderAt semantics: if we get ErrUnexpectedEOF, it means
		// we read some data but hit EOF before filling the buffer. According to
		// io.ReaderAt contract, we should return the partial data with io.EOF.
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return n, io.EOF
		}
		return n, fmt.Errorf("read from range reader: %w", err)
	}

	return n, nil
}

func (g *gcsChunkReader) Close() error {
	return nil
}
