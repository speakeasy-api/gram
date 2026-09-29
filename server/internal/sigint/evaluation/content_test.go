package evaluation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
)

type memoryBlobs map[string][]byte

func (m memoryBlobs) Read(_ context.Context, u *url.URL) (io.ReadCloser, error) {
	data, ok := m[u.String()]
	if !ok {
		return nil, fmt.Errorf("asset unavailable")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func reference(uri, media string, data []byte) *conversationv1.Message_ContentReference {
	r := &conversationv1.Message_ContentReference{}
	r.SetUri(uri)
	r.SetMediaType(media)
	r.SetSizeBytes(uint64(len(data)))
	sum := sha256.Sum256(data)
	r.SetSha256(sum[:])
	return r
}

func TestInputInlineAndAssetResolveIdentically(t *testing.T) {
	t.Parallel()
	m := message()
	state, err := input(t.Context(), nil, m)
	require.NoError(t, err)
	data, err := proto.Marshal(m.GetBody())
	require.NoError(t, err)
	uri := "gs://test-bucket/" + m.GetProjectId() + "/body.pb"
	m.SetBodyReference(reference(uri, "application/x-protobuf", data))
	resolved, err := input(t.Context(), memoryBlobs{uri: data}, m)
	require.NoError(t, err)
	require.Equal(t, state, resolved)
}

func TestInputOriginalContentNotDuplicatedAndNumbersPreserved(t *testing.T) {
	t.Parallel()
	m := message()
	body := m.GetBody()
	body.SetSourceContentJson([]byte(`{"count":9007199254740993,"text":"original"}`))
	tool := &conversationv1.Message_ToolCall{}
	tool.SetId("tool")
	tool.SetName("lookup")
	tool.SetArgumentsJson(`{"incomplete":`)
	part := &conversationv1.Message_Part{}
	part.SetToolCall(tool)
	body.SetParts(append(body.GetParts(), part))
	state, err := input(t.Context(), nil, m)
	require.NoError(t, err)
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "9007199254740993")
	require.NotContains(t, string(encoded), "failed payment")
	require.Contains(t, string(encoded), "lookup")
}

func TestInputRejectsCrossProjectAndIntegrityFailure(t *testing.T) {
	t.Parallel()
	m := message()
	data, err := proto.Marshal(m.GetBody())
	require.NoError(t, err)
	uri := "gs://test-bucket/another-project/body.pb"
	m.SetBodyReference(reference(uri, "application/x-protobuf", data))
	_, err = input(t.Context(), nil, m)
	var permanentFailure *permanentError
	require.ErrorAs(t, err, &permanentFailure)
	require.Equal(t, "invalid_reference", permanentFailure.reason)
	uri = "gs://test-bucket/" + m.GetProjectId() + "/body.pb"
	m.SetBodyReference(reference(uri, "application/x-protobuf", data))
	_, err = input(t.Context(), memoryBlobs{uri: []byte("corrupt")}, m)
	require.ErrorAs(t, err, &permanentFailure)
	require.Equal(t, "asset_integrity", permanentFailure.reason)
}

func TestInputAssetUnavailableIsRetryable(t *testing.T) {
	t.Parallel()
	m := message()
	m.SetBodyReference(reference("gs://test-bucket/"+m.GetProjectId()+"/body.pb", "application/x-protobuf", nil))
	_, err := input(t.Context(), memoryBlobs{}, m)
	require.Error(t, err)
	var permanentFailure *permanentError
	require.NotErrorAs(t, err, &permanentFailure)
}

func TestInputResolvesTextPartAndRejectsBinary(t *testing.T) {
	t.Parallel()
	m := message()
	uri := "gs://test-bucket/" + m.GetProjectId() + "/part.txt"
	data := []byte("asset-backed text")
	part := &conversationv1.Message_Part{}
	part.SetContentReference(reference(uri, "text/plain; charset=utf-8", data))
	m.GetBody().SetParts([]*conversationv1.Message_Part{part})
	state, err := input(t.Context(), memoryBlobs{uri: data}, m)
	require.NoError(t, err)
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "asset-backed text")
	part.GetContentReference().SetMediaType("image/png")
	_, err = input(t.Context(), memoryBlobs{uri: data}, m)
	var permanentFailure *permanentError
	require.ErrorAs(t, err, &permanentFailure)
	require.Equal(t, "unsupported_content", permanentFailure.reason)
}
