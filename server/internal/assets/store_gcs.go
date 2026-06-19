package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type GCSBlobStore struct {
	logger    *slog.Logger
	client    *storage.Client
	bucket    *storage.BucketHandle
	bucketURI *url.URL
}

var _ BlobStore = (*GCSBlobStore)(nil)

func NewGCSBlobStore(ctx context.Context, logger *slog.Logger, bucketURI string) (*GCSBlobStore, error) {
	client, err := storage.NewGRPCClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create gcs client: %w", err)
	}

	uri, err := url.Parse(bucketURI)
	if err != nil {
		return nil, fmt.Errorf("parse bucket uri: %w", err)
	}

	bucket := client.Bucket(uri.Hostname())

	return &GCSBlobStore{
		logger:    logger.With(attr.SlogComponent("blob_store_gcs")),
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

// Write attaches a DoesNotExist:true precondition so retries and concurrent
// racers cannot produce duplicates: a retry after a transient failure can't
// re-create the object, and concurrent writers racing to the same
// content-addressable key won't trip GCS's per-object 1-write/sec limit with
// ResourceExhausted — the loser instead returns FailedPrecondition, which
// gcsConditionalWriter treats as success since the object is already in the
// desired state. The precondition also flips the SDK's default
// RetryIdempotent policy on for this Writer, so transient errors retry
// automatically.
func (gbs *GCSBlobStore) Write(ctx context.Context, subpath string, contentType string, contentLength int64) (io.WriteCloser, *url.URL, error) {
	uri, err := gbs.getBucketURI(subpath)
	if err != nil {
		return nil, nil, fmt.Errorf("generate asset path: %w", err)
	}

	obj := gbs.bucket.Object(strings.TrimPrefix(uri.Path, "/")).If(storage.Conditions{
		GenerationMatch:        0,
		GenerationNotMatch:     0,
		DoesNotExist:           true,
		MetagenerationMatch:    0,
		MetagenerationNotMatch: 0,
	})
	w := obj.NewWriter(ctx)
	w.ContentType = contentType

	return &gcsConditionalWriter{Writer: w}, uri, nil
}

// gcsConditionalWriter wraps storage.Writer so a FailedPrecondition on Close
// (from the DoesNotExist precondition) is reported as success — the object is
// already in the desired state, which is what the caller wanted.
type gcsConditionalWriter struct {
	*storage.Writer
}

func (g *gcsConditionalWriter) Close() error {
	err := g.Writer.Close()
	if err == nil {
		return nil
	}
	if isPreconditionFailed(err) {
		return nil
	}
	return fmt.Errorf("close gcs writer: %w", err)
}

func isPreconditionFailed(err error) bool {
	if status.Code(err) == codes.FailedPrecondition {
		return true
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusPreconditionFailed {
		return true
	}
	return false
}

func (gbs *GCSBlobStore) PresignRead(ctx context.Context, subpath string, ttl time.Duration) (*url.URL, error) {
	uri, err := gbs.getBucketURI(subpath)
	if err != nil {
		return nil, fmt.Errorf("generate asset path: %w", err)
	}

	signed, err := gbs.bucket.SignedURL(strings.TrimPrefix(uri.Path, "/"), &storage.SignedURLOptions{
		Expires: time.Now().Add(ttl),
		Method:  http.MethodGet,
	})
	if err != nil {
		return nil, fmt.Errorf("generate signed gcs url: %w", err)
	}

	u, err := url.Parse(signed)
	if err != nil {
		return nil, fmt.Errorf("parse signed gcs url: %w", err)
	}

	return u, nil
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
