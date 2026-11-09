package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3HTTPClientFunc func(*http.Request) (*http.Response, error)

func (f s3HTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestS3BlobStore(t *testing.T, do s3HTTPClientFunc) *S3BlobStore {
	t.Helper()

	bucketURI, err := url.Parse("s3://test-bucket")
	if err != nil {
		t.Fatal(err)
	}

	return &S3BlobStore{
		client: s3.New(s3.Options{
			Region:      "us-east-1",
			Credentials: aws.AnonymousCredentials{},
			HTTPClient:  do,
			Retryer:     aws.NopRetryer{},
		}),
		bucket:    "test-bucket",
		bucketURI: bucketURI,
	}
}

func successfulS3Response() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func TestS3WriteCloseWaitsForPutObject(t *testing.T) {
	t.Parallel()
	putStarted := make(chan struct{})
	completePut := make(chan struct{})
	store := newTestS3BlobStore(t, func(req *http.Request) (*http.Response, error) {
		close(putStarted)
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		<-completePut
		return successfulS3Response(), nil
	})

	w, _, err := store.Write(context.Background(), "close-blocks", "text/plain", 4)
	if err != nil {
		t.Fatal(err)
	}
	<-putStarted
	if _, err := io.WriteString(w, "data"); err != nil {
		t.Fatal(err)
	}

	closed := make(chan error, 1)
	go func() { closed <- w.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned before PutObject completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(completePut)
	if err := <-closed; err != nil {
		t.Fatalf("Close returned an error: %v", err)
	}
}

func TestS3WriteClosePropagatesPutObjectError(t *testing.T) {
	t.Parallel()
	putErr := errors.New("put failed")
	store := newTestS3BlobStore(t, func(req *http.Request) (*http.Response, error) {
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		return nil, putErr
	})

	w, _, err := store.Write(context.Background(), "put-error", "text/plain", 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "data"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); !errors.Is(err, putErr) {
		t.Fatalf("Close error = %v, want wrapped %v", err, putErr)
	}
}

func TestS3WriteEarlyPutObjectFailureUnblocksWrite(t *testing.T) {
	t.Parallel()
	putErr := errors.New("early put failure")
	store := newTestS3BlobStore(t, func(*http.Request) (*http.Response, error) {
		return nil, putErr
	})

	w, _, err := store.Write(context.Background(), "early-error", "application/octet-stream", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	written, err := w.Write(make([]byte, 1<<20))
	if !errors.Is(err, putErr) {
		t.Fatalf("Write error = %v, want wrapped %v", err, putErr)
	}
	if written == 1<<20 {
		t.Fatal("Write unexpectedly consumed the whole body")
	}
	if err := w.Close(); !errors.Is(err, putErr) {
		t.Fatalf("Close error = %v, want wrapped %v", err, putErr)
	}
}

func TestS3WritePreservesBody(t *testing.T) {
	t.Parallel()
	want := "the complete upload body"
	var got string
	store := newTestS3BlobStore(t, func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		got = string(body)
		return successfulS3Response(), nil
	})

	w, _, err := store.Write(context.Background(), "body", "text/plain", int64(len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, want[:7]); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, want[7:]); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("uploaded body = %q, want %q", got, want)
	}
}
