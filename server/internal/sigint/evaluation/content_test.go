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

func TestInputResolvesFilesystemWriterLocator(t *testing.T) {
	t.Parallel()
	m := message()
	row := targetContent(storedMessage(m))
	data := []byte(`{"text":"original"}`)

	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, root.Close()) })
	store := assets.NewFSBlobStore(testenv.NewLogger(t), root)

	w, locator, err := store.Write(t.Context(), m.GetProjectId()+"/content.json", "application/json", int64(len(data)))
	require.NoError(t, err)

	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	row.ContentRaw = nil
	row.ContentAssetUrl = locator.String()

	got, err := inputWithBudget(t.Context(), store, m.GetProjectId(), row, maxContentBytes)
	require.NoError(t, err)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	testenv.RequireYAMLGolden(t, "testdata/state/filesystem-content.golden.yaml", encoded)

	for _, uri := range []string{"file:another-project/content.json", "file:" + m.GetProjectId() + "/../content.json", "file:" + m.GetProjectId() + "/%2e%2e/content.json"} {
		row.ContentAssetUrl = uri
		_, err := inputWithBudget(t.Context(), store, m.GetProjectId(), row, maxContentBytes)
		var failed *permanentError
		require.ErrorAs(t, err, &failed)
		require.Equal(t, "invalid_reference", failed.reason)
	}
}

func TestInputAttachmentFailureAndSizeLimit(t *testing.T) {
	t.Parallel()
	m := message()
	row := targetContent(storedMessage(m))
	uri := "gs://test-bucket/" + m.GetProjectId() + "/part.txt"
	row.AttachmentUris = []string{uri}

	_, err := inputWithBudget(t.Context(), memoryBlobs{}, m.GetProjectId(), row, maxContentBytes)
	require.Error(t, err)

	var failed *permanentError
	require.NotErrorAs(t, err, &failed)

	_, err = inputWithBudget(t.Context(), memoryBlobs{uri: bytes.Repeat([]byte("x"), maxContentBytes)}, m.GetProjectId(), row, maxContentBytes)
	require.ErrorAs(t, err, &failed)
	require.Equal(t, "content_too_large", failed.reason)

	_, err = inputWithBudget(t.Context(), memoryBlobs{uri: {0xff}}, m.GetProjectId(), row, maxContentBytes)
	require.ErrorAs(t, err, &failed)
	require.Equal(t, "invalid_content", failed.reason)
}

func TestInputNullOriginalContentIsOmitted(t *testing.T) {
	t.Parallel()
	for _, spilled := range []bool{false, true} {
		t.Run(fmt.Sprintf("spilled=%t", spilled), func(t *testing.T) {
			t.Parallel()
			m := message()
			row := targetContent(storedMessage(m))
			uri := "gs://test-bucket/" + m.GetProjectId() + "/content.json"
			if spilled {
				row.ContentAssetUrl = uri
			} else {
				row.ContentRaw = []byte("null")
			}
			state, err := inputWithBudget(t.Context(), memoryBlobs{uri: []byte("null")}, m.GetProjectId(), row, maxContentBytes)
			require.NoError(t, err)
			data, err := json.Marshal(state)
			require.NoError(t, err)
			testenv.RequireYAMLGolden(t, "testdata/state/null-original-content.golden.yaml", data)
		})
	}
}
