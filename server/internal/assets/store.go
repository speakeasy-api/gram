package assets

import (
	"context"
	"io"
	"net/url"
	"time"
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
