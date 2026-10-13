package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type memoryBlobs map[string][]byte

func (m memoryBlobs) Read(_ context.Context, u *url.URL) (io.ReadCloser, error) {
	data, ok := m[u.String()]
	if !ok {
		return nil, fmt.Errorf("asset unavailable")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestInputOriginalContentNotDuplicatedAndNumbersPreserved(t *testing.T) {
	t.Parallel()
	m := message()
	row := storedMessage(m)
	row.ChatMessage.ContentRaw = []byte(`{"count":9007199254740993,"text":"original"}`)
	row.ChatMessage.ToolCalls = []byte(`[{"id":"tool","function":{"name":"lookup","arguments":"{\"incomplete\":"}}]`)

	state, err := input(t.Context(), nil, m.GetProjectId(), row)
	require.NoError(t, err)

	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "9007199254740993")
	require.NotContains(t, string(encoded), "failed payment")
	require.Contains(t, string(encoded), "lookup")
}

func TestInputSplitRowsExcludeArchivalSiblingContent(t *testing.T) {
	t.Parallel()
	m := message()
	row := storedMessage(m)
	row.RowLocalContent = true
	row.ChatMessage.ContentRaw = []byte(`[{"type":"text","text":"sibling content"}]`)

	state, err := input(t.Context(), nil, m.GetProjectId(), row)
	require.NoError(t, err)

	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "failed payment")
	require.NotContains(t, string(encoded), "sibling content")
}

func TestInputResolvesFilesystemWriterLocator(t *testing.T) {
	t.Parallel()
	m := message()
	row := storedMessage(m)
	data := []byte(`{"text":"original"}`)
	row.ChatMessage.ContentRaw = data

	expected, err := input(t.Context(), nil, m.GetProjectId(), row)
	require.NoError(t, err)

	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, root.Close()) })
	store := assets.NewFSBlobStore(testenv.NewLogger(t), root)

	w, locator, err := store.Write(t.Context(), m.GetProjectId()+"/content.json", "application/json", int64(len(data)))
	require.NoError(t, err)

	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	row.ChatMessage.ContentRaw = nil
	row.ChatMessage.ContentAssetUrl = conv.ToPGText(locator.String())

	got, err := input(t.Context(), store, m.GetProjectId(), row)
	require.NoError(t, err)
	require.Equal(t, expected, got)

	for _, uri := range []string{"file:another-project/content.json", "file:" + m.GetProjectId() + "/../content.json", "file:" + m.GetProjectId() + "/%2e%2e/content.json"} {
		row.ChatMessage.ContentAssetUrl = conv.ToPGText(uri)
		_, err := input(t.Context(), store, m.GetProjectId(), row)
		var failed *permanentError
		require.ErrorAs(t, err, &failed)
		require.Equal(t, "invalid_reference", failed.reason)
	}
}

func TestInputAttachmentFailureAndSizeLimit(t *testing.T) {
	t.Parallel()
	m := message()
	row := storedMessage(m)
	uri := "gs://test-bucket/" + m.GetProjectId() + "/part.txt"
	row.AttachmentUris = []string{uri}

	_, err := input(t.Context(), memoryBlobs{}, m.GetProjectId(), row)
	require.Error(t, err)

	var failed *permanentError
	require.NotErrorAs(t, err, &failed)

	state, err := input(t.Context(), memoryBlobs{uri: []byte("attachment text")}, m.GetProjectId(), row)
	require.NoError(t, err)

	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "attachment text")

	_, err = input(t.Context(), memoryBlobs{uri: bytes.Repeat([]byte("x"), maxContentBytes)}, m.GetProjectId(), row)
	require.ErrorAs(t, err, &failed)
	require.Equal(t, "content_too_large", failed.reason)

	_, err = input(t.Context(), memoryBlobs{uri: {0xff}}, m.GetProjectId(), row)
	require.ErrorAs(t, err, &failed)
	require.Equal(t, "invalid_content", failed.reason)
}
