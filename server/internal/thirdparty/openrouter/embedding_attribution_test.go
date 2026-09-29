package openrouter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestChatClient_CreateEmbeddings_OrganizationAttribution(t *testing.T) {
	// The SDK creates its own HTTP client. Keep this test non-parallel while
	// replacing the default transport so no request can reach OpenRouter.
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	for _, tt := range []struct {
		name    string
		orgID   string
		keyType KeyType
	}{
		{name: "chat", orgID: "test-org-chat", keyType: KeyTypeChat},
		{name: "internal", orgID: "test-org-internal", keyType: KeyTypeInternal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			transport := &embeddingAttributionTransport{}
			http.DefaultTransport = transport
			provisioner := &mockProvisioner{apiKey: "test-api-key"}
			client := &ChatClient{
				logger:      testenv.NewLogger(t),
				keyResolver: &PlatformKeyResolver{Provisioner: provisioner},
			}

			vectors, err := client.CreateEmbeddings(t.Context(), tt.orgID, openAITextEmbedding3Small, []string{"hello"}, WithEmbeddingKeyType(tt.keyType))
			require.NoError(t, err)
			require.Equal(t, [][]float32{{0.25, 0.5}}, vectors)
			require.Equal(t, tt.orgID, transport.user, "user must be the organization ID, not an individual user ID")
			require.Equal(t, []KeyType{tt.keyType}, provisioner.ProvisionedKeyTypes())
		})
	}
}

type embeddingAttributionTransport struct {
	user string
}

func (t *embeddingAttributionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	defer req.Body.Close()
	var body struct {
		User string `json:"user"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode embedding request: %w", err)
	}
	t.user = body.User
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"object": "list",
			"data": [{"object": "embedding", "index": 0, "embedding": [0.25, 0.5]}],
			"model": "openai/text-embedding-3-small",
			"usage": {"prompt_tokens": 1, "total_tokens": 1}
		}`)),
		Request: req,
	}, nil
}
