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
	// Location resolves the exact durable URL for an object path without
	// creating the object or starting backend I/O.
	Location(objectPath string) (*url.URL, error)
	// Write writes the object such that retries and concurrent racers cannot
	// produce duplicate work. Intended for content-addressable callers (path
	// derived from a hash of the bytes) where overwriting an existing object
	// is wasted work and, on GCS, hot-keys can hit the per-object 1-write/sec
	// rate limit. Backends that support server-side preconditions (GCS) attach
	// a "create only if absent" condition and report "already exists" as
	// success; backends without native support overwrite, which produces an
	// identical result for content-addressable payloads. A successful Close
	// guarantees that the backend has completed the write. Backend failures are
	// returned by Write or Close rather than being reported asynchronously.
	Write(ctx context.Context, objectPath string, contentType string, contentLength int64) (io.WriteCloser, *url.URL, error)
	PresignRead(ctx context.Context, objectPath string, ttl time.Duration) (*url.URL, error)
	// Delete removes the object. Idempotent: deleting an object that does not
	// exist reports success, so burn-after-read and expiry cleanup callers can
	// safely retry it.
	Delete(ctx context.Context, objectURL *url.URL) error
}
