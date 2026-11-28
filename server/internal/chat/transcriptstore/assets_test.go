package transcriptstore

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// memoryBlobs is an in-memory AssetBlobs whose object URLs are mem:///<path>.
type memoryBlobs struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMemoryBlobs() *memoryBlobs {
	return &memoryBlobs{mu: sync.Mutex{}, objects: map[string][]byte{}}
}

func (m *memoryBlobs) Exists(_ context.Context, u *url.URL) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[u.Path]
	return ok, nil
}

func (m *memoryBlobs) Read(_ context.Context, u *url.URL) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return io.NopCloser(bytes.NewReader(m.objects[u.Path])), nil
}

type memoryWriter struct {
	bytes.Buffer
	commit func([]byte)
}

func (w *memoryWriter) Close() error {
	w.commit(w.Bytes())
	return nil
}

func (m *memoryBlobs) Write(_ context.Context, objectPath string, _ string, _ int64) (io.WriteCloser, *url.URL, error) {
	key := "/" + strings.TrimPrefix(objectPath, "/")
	w := &memoryWriter{Buffer: bytes.Buffer{}, commit: func(b []byte) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.objects[key] = append([]byte(nil), b...)
	}}
	return w, &url.URL{Scheme: "mem", Path: key}, nil
}

func newAssetFixture(t *testing.T) (*fixture, *AssetStore, *memoryBlobs) {
	t.Helper()
	f := setup(t, true)
	blobs := newMemoryBlobs()
	store, err := NewAssetStore(blobs, f.keys.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	return f, store, blobs
}

func TestOpenAssetRejectsABodyOverTheReadersLimit(t *testing.T) {
	t.Parallel()
	f, store, _ := newAssetFixture(t)
	body := []byte(strings.Repeat("invented ledger line\n", 100))
	hash := transcriptcrypto.ContentHash(body)
	identity := transcriptcrypto.AssetIdentity{Kind: transcriptcrypto.AssetContentPart, ProjectID: f.project, ChatID: f.chat, SHA256: hash}
	objectPath := path.Join(f.project.String(), "chats", f.chat.String(), "content-parts", hash+".txt")
	require.NoError(t, store.Seal(t.Context(), f.org, identity, objectPath, body))
	objectURL := &url.URL{Scheme: "mem", Path: "/" + objectPath}

	opened, found, err := store.Open(t.Context(), f.org, identity, objectURL, int64(len(body)))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, body, opened)

	// The sealed object fits the read allowance for a slightly smaller
	// limit, but the opened body does not.
	opened, found, err = store.Open(t.Context(), f.org, identity, objectURL, int64(len(body)-1))
	require.ErrorIs(t, err, ErrStale)
	require.False(t, found)
	require.Nil(t, opened)
}

func TestRefuseUnverifiableFailsOnlyWhenACopyExists(t *testing.T) {
	t.Parallel()
	_, store, blobs := newAssetFixture(t)
	objectURL := &url.URL{Scheme: "mem", Path: "/project/chats/chat/notes.txt"}
	require.NoError(t, store.RefuseUnverifiable(t.Context(), objectURL))

	w, _, err := blobs.Write(t.Context(), "project/chats/chat/notes.txt"+SealedAssetSuffix, "", 0)
	require.NoError(t, err)
	_, err = w.Write([]byte("sealed"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.ErrorIs(t, store.RefuseUnverifiable(t.Context(), objectURL), ErrStale)
}

func TestSealedAssetURLMatchesSealedAssetPath(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"gs://bucket/project/chats/chat/abc.json", "file://project/chats/chat/abc.json", "file:abc.json"} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, raw+SealedAssetSuffix, SealedAssetURL(u).String(), raw)
	}
}
