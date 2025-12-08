package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

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
