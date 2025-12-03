package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
	Write(ctx context.Context, objectPath string, contentType string, contentLength int64) (io.WriteCloser, *url.URL, error)
	PresignRead(ctx context.Context, objectPath string, ttl time.Duration) (*url.URL, error)
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

func (gbs *GCSBlobStore) Write(ctx context.Context, subpath string, contentType string, contentLength int64) (io.WriteCloser, *url.URL, error) {
	uri, err := gbs.getBucketURI(subpath)
	if err != nil {
		return nil, nil, fmt.Errorf("generate asset path: %w", err)
	}

	w := gbs.bucket.Object(strings.TrimPrefix(uri.Path, "/")).NewWriter(ctx)
	w.ContentType = contentType

	return w, uri, nil
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

type S3BlobStore struct {
	logger       *slog.Logger
	client       *s3.Client
	bucket       string
	bucketURI    *url.URL
	usePathStyle bool
}

var _ BlobStore = (*S3BlobStore)(nil)

type S3BlobStoreOptions struct {
	BaseEndpoint string
	Region       string
	UsePathStyle bool
	AccessKey    string
	AccessSecret string
}

func NewS3BlobStore(ctx context.Context, logger *slog.Logger, bucketURI string, opts S3BlobStoreOptions) (*S3BlobStore, error) {
	loadOptions := []func(*config.LoadOptions) error{
		config.WithRegion(opts.Region),
	}
	if opts.AccessKey != "" && opts.AccessSecret != "" {
		loadOptions = append(loadOptions, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.AccessKey, opts.AccessSecret, ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	clientOpts := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = opts.UsePathStyle
		},
	}

	if opts.BaseEndpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(opts.BaseEndpoint)
		})
	}

	client := s3.NewFromConfig(cfg, clientOpts...)

	uri, err := url.Parse(bucketURI)
	if err != nil {
		return nil, fmt.Errorf("parse bucket uri: %w", err)
	}

	return &S3BlobStore{
		logger:       logger.With(attr.SlogComponent("blob-store-s3")),
		client:       client,
		bucket:       uri.Hostname(),
		bucketURI:    uri,
		usePathStyle: opts.UsePathStyle,
	}, nil
}

func (sbs *S3BlobStore) getPath(u *url.URL) (string, error) {
	if u.Scheme != "s3" {
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	bu := sbs.bucketURI.String()
	if !strings.HasSuffix(bu, "/") {
		bu += "/"
	}

	if !strings.HasPrefix(u.String(), bu) {
		return "", fmt.Errorf("unauthorized access")
	}

	path := strings.TrimPrefix(u.Path, "/")

	// For path-style URLs (s3://bucket/path/to/object), the bucket is in the hostname
	// and we need to strip it from the path if it's there
	if sbs.usePathStyle && strings.HasPrefix(path, sbs.bucket+"/") {
		path = strings.TrimPrefix(path, sbs.bucket+"/")
	}

	return path, nil
}

func (sbs *S3BlobStore) getBucketURI(subpath string) (*url.URL, error) {
	noPrefix := strings.TrimPrefix(subpath, sbs.bucketURI.Path)
	noSlash := strings.TrimPrefix(noPrefix, "/")
	u := sbs.bucketURI.JoinPath(noSlash)
	us := u.String()
	if us == "" || !strings.HasPrefix(us, sbs.bucketURI.String()) || us == sbs.bucketURI.String() {
		return nil, fmt.Errorf("invalid path: %s", subpath)
	}

	return u, nil
}

func (sbs *S3BlobStore) Exists(ctx context.Context, u *url.URL) (bool, error) {
	subpath, err := sbs.getPath(u)
	if err != nil {
		return false, fmt.Errorf("generate asset path: %w", err)
	}

	_, err = sbs.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(sbs.bucket),
		Key:    aws.String(subpath),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("head object: %w", err)
	}

	return true, nil
}

func (sbs *S3BlobStore) Read(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	subpath, err := sbs.getPath(u)
	if err != nil {
		return nil, fmt.Errorf("generate asset path: %w", err)
	}

	result, err := sbs.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(sbs.bucket),
		Key:    aws.String(subpath),
	})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}

	return result.Body, nil
}

func (sbs *S3BlobStore) ReadAt(ctx context.Context, u *url.URL) (ReaderAtCloser, int64, error) {
	subpath, err := sbs.getPath(u)
	if err != nil {
		return nil, 0, fmt.Errorf("generate asset path: %w", err)
	}

	headResult, err := sbs.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(sbs.bucket),
		Key:    aws.String(subpath),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("head object: %w", err)
	}

	return &s3ChunkReader{
		logger:     sbs.logger,
		client:     sbs.client,
		bucket:     sbs.bucket,
		objectPath: subpath,
		context:    func() context.Context { return ctx },
	}, aws.ToInt64(headResult.ContentLength), nil
}

func (sbs *S3BlobStore) Write(ctx context.Context, subpath string, contentType string, contentLength int64) (io.WriteCloser, *url.URL, error) {
	uri, err := sbs.getBucketURI(subpath)
	if err != nil {
		return nil, nil, fmt.Errorf("generate asset path: %w", err)
	}

	pr, pw := io.Pipe()

	go func() {
		defer func() {
			if closeErr := pw.Close(); closeErr != nil {
				sbs.logger.ErrorContext(ctx, "failed to close pipe writer", attr.SlogError(closeErr))
			}
		}()
		_, err := sbs.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(sbs.bucket),
			Key:           aws.String(strings.TrimPrefix(uri.Path, "/")),
			Body:          pr,
			ContentType:   aws.String(contentType),
			ContentLength: conv.Ptr(contentLength),
		})
		if err != nil {
			sbs.logger.ErrorContext(ctx, "failed to put object to s3", attr.SlogError(err))
		}
	}()

	return pw, uri, nil
}

func (sbs *S3BlobStore) PresignRead(ctx context.Context, subpath string, ttl time.Duration) (*url.URL, error) {
	uri, err := sbs.getBucketURI(subpath)
	if err != nil {
		return nil, fmt.Errorf("generate asset path: %w", err)
	}

	presignClient := s3.NewPresignClient(sbs.client)
	presignResult, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(sbs.bucket),
		Key:    aws.String(strings.TrimPrefix(uri.Path, "/")),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return nil, fmt.Errorf("presign get object: %w", err)
	}

	u, err := url.Parse(presignResult.URL)
	if err != nil {
		return nil, fmt.Errorf("parse presigned url: %w", err)
	}

	return u, nil
}

type s3ChunkReader struct {
	logger     *slog.Logger
	client     *s3.Client
	bucket     string
	objectPath string
	context    func() context.Context
}

func (s *s3ChunkReader) ReadAt(p []byte, offset int64) (int, error) {
	ctx := s.context()

	endOffset := offset + int64(len(p)) - 1
	rangeHeader := fmt.Sprintf("bytes=%d-%d", offset, endOffset)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectPath),
		Range:  aws.String(rangeHeader),
	})
	if err != nil {
		return 0, fmt.Errorf("get object with range: %w", err)
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return result.Body.Close()
	})

	n, err := io.ReadFull(result.Body, p)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return n, io.EOF
		}
		return n, fmt.Errorf("read from range reader: %w", err)
	}

	return n, nil
}

func (s *s3ChunkReader) Close() error {
	return nil
}

func isS3NotFoundError(err error) bool {
	// Check if the error is a "not found" error
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}

	// Also check for NoSuchKey (some S3-compatible services use this)
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}

	// Check for 404 status code (alternative method)
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
		return true
	}

	return true
}

type FlyTigrisStore struct {
	BlobStore

	presignHost string
}

type FlyTigrisStoreOption func(*FlyTigrisStore)

func WithTigrisPresignHost(host string) FlyTigrisStoreOption {
	return func(fts *FlyTigrisStore) {
		fts.presignHost = host
	}
}

func NewFlyTigrisStore(blobStore BlobStore, opts ...FlyTigrisStoreOption) *FlyTigrisStore {
	fts := &FlyTigrisStore{
		BlobStore:   blobStore,
		presignHost: "",
	}

	for _, opt := range opts {
		opt(fts)
	}

	return fts
}

func (fts *FlyTigrisStore) PresignRead(ctx context.Context, subpath string, ttl time.Duration) (*url.URL, error) {
	u, err := fts.BlobStore.PresignRead(ctx, subpath, ttl)
	if err != nil {
		return nil, fmt.Errorf("create presigned s3 url: %w", err)
	}

	if fts.presignHost != "" {
		u.Host = fts.presignHost
	}

	return u, nil
}
