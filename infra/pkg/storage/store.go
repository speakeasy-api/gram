package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	gcs "cloud.google.com/go/storage"
)

// Object describes a unique immutable Parquet object, never an overwrite.
type Object struct {
	// Bucket is the deployment-resolved physical name.
	Bucket string

	// Name includes the marker prefix, Hive suffix and unique filename.
	Name string

	// Metadata carries schema identity independently of the directory structure.
	Metadata map[string]string
}

// Store commits the object only if encode and the durable close both succeed.
// Errors and panics must abort the write; nil means the complete object exists.
type Store interface {
	Write(context.Context, Object, func(io.Writer) error) error
}

// GCSStore writes create-only objects; it never creates buckets or deletes data.
type GCSStore struct {
	// Client uses the consuming process's credentials and lifecycle.
	Client *gcs.Client
}

// gcsChunkBytes bounds each concurrent resumable upload buffer to one MiB.
const gcsChunkBytes = 1 << 20

// Write commits after the Parquet writer has successfully finished its footer.
func (s *GCSStore) Write(ctx context.Context, object Object, encode func(io.Writer) error) error {
	if s.Client == nil {
		return errors.New("GCS client is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w := s.Client.Bucket(object.Bucket).Object(object.Name).If(gcs.Conditions{DoesNotExist: true}).NewWriter(ctx)
	// One MiB per concurrent upload bounds the SDK's resumable-upload buffers.
	w.ChunkSize = gcsChunkBytes
	w.ContentType = "application/vnd.apache.parquet"
	w.Metadata = object.Metadata
	committed := false
	defer func() {
		if !committed {
			cancel()
			_ = w.CloseWithError(errors.New("storage object write aborted"))
		}
	}()
	if err := encode(w); err != nil {
		return fmt.Errorf("encode storage object: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("commit storage object: %w", err)
	}
	committed = true
	return nil
}
