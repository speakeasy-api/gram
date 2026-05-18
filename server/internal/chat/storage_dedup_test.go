package chat_test

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// countingBlobStore wraps a BlobStore and records every Write call so tests
// can assert that content-addressable dedup avoids redundant uploads.
type countingBlobStore struct {
	inner  assets.BlobStore
	mu     sync.Mutex
	writes int
}

func newCountingBlobStore(t *testing.T) *countingBlobStore {
	t.Helper()
	return &countingBlobStore{inner: assetstest.NewTestBlobStore(t)}
}

func (c *countingBlobStore) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

func (c *countingBlobStore) Exists(ctx context.Context, u *url.URL) (bool, error) {
	ok, err := c.inner.Exists(ctx, u)
	if err != nil {
		return false, fmt.Errorf("countingBlobStore exists: %w", err)
	}
	return ok, nil
}

func (c *countingBlobStore) Read(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	rc, err := c.inner.Read(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("countingBlobStore read: %w", err)
	}
	return rc, nil
}

func (c *countingBlobStore) ReadAt(ctx context.Context, u *url.URL) (assets.ReaderAtCloser, int64, error) {
	r, n, err := c.inner.ReadAt(ctx, u)
	if err != nil {
		return nil, 0, fmt.Errorf("countingBlobStore readAt: %w", err)
	}
	return r, n, nil
}

func (c *countingBlobStore) Write(ctx context.Context, p string, ct string, cl int64) (io.WriteCloser, *url.URL, error) {
	c.mu.Lock()
	c.writes++
	c.mu.Unlock()
	w, u, err := c.inner.Write(ctx, p, ct, cl)
	if err != nil {
		return nil, nil, fmt.Errorf("countingBlobStore write: %w", err)
	}
	return w, u, nil
}

func (c *countingBlobStore) PresignRead(ctx context.Context, p string, ttl time.Duration) (*url.URL, error) {
	u, err := c.inner.PresignRead(ctx, p, ttl)
	if err != nil {
		return nil, fmt.Errorf("countingBlobStore presignRead: %w", err)
	}
	return u, nil
}

// Rows with identical content within a single batch hash to the same
// content-addressable asset path. storeMessages must dispatch a single upload
// per unique content and fan the resulting asset URL out to every duplicate
// row — otherwise concurrent writes to the same GCS object trip the per-object
// 1-write/sec rate limit (AGE-2319).
func TestStoreMessages_DeduplicatesContentAddressableWrites(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)

	counting := newCountingBlobStore(t)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, counting)
	t.Cleanup(func() { _ = shutdown(t.Context()) })

	s := chat.NewChatMessageCaptureStrategy(testenv.NewLogger(t), conn, writer)
	chatID := uuid.New()

	// Two user messages with identical content + one distinct system message.
	// Three rows total; two unique contents.
	req := makeRequest(chatID, projectID, orgID,
		openrouter.CreateMessageUser("hello"),
		openrouter.CreateMessageUser("hello"),
		openrouter.CreateMessageSystem("preamble"),
	)

	_, err := s.StartOrResumeChat(ctx, req)
	require.NoError(t, err)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 3)

	require.Equal(t, 2, counting.writeCount(), "one Write per unique content, not per row")

	var userURLs []string
	for _, r := range rows {
		if r.Role == "user" {
			userURLs = append(userURLs, r.ContentAssetUrl.String)
		}
	}
	require.Len(t, userURLs, 2)
	require.NotEmpty(t, userURLs[0])
	require.Equal(t, userURLs[0], userURLs[1], "duplicate-content rows must share the same content_asset_url")
}
