package openrouter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
)

func TestKeyTypeOrDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, KeyTypeChat, KeyType("").OrDefault())
	require.Equal(t, KeyTypeChat, KeyTypeChat.OrDefault())
	require.Equal(t, KeyTypeInternal, KeyTypeInternal.OrDefault())
}

func TestBillableKeyTypes(t *testing.T) {
	t.Parallel()

	require.Equal(t, []KeyType{KeyTypeChat, KeyTypeInternal}, BillableKeyTypes())
	require.Equal(t, []string{"chat", "internal"}, BillableKeyTypeStrings())
	require.Equal(t, "4b4e792daf43040a6f92b112a281187144b92cc902d5b355056f28a0c2ad6894", BillableKeyPolicyFingerprint())
	require.True(t, KeyTypeChat.IsBillable())
	require.True(t, KeyTypeInternal.IsBillable())
	require.False(t, KeyType("future").IsBillable())

	keyTypes := BillableKeyTypes()
	keyTypes[0] = "changed"
	require.Equal(t, KeyTypeChat, BillableKeyTypes()[0], "callers cannot mutate billing policy")
}

func TestKeyTypeValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, KeyType("").Validate(), "zero value resolves to chat")
	require.NoError(t, KeyTypeChat.Validate())
	require.NoError(t, KeyTypeInternal.Validate())
	require.Error(t, KeyType("internl").Validate(), "a typo must not mint a third key type")
}

func TestUpstreamKeyIdentity(t *testing.T) {
	t.Parallel()

	// The chat key format is load-bearing: upstream keys already exist under
	// these exact names.
	name, label := upstreamKeyIdentity("prod", "org-1", "acme", KeyTypeChat)
	require.Equal(t, "gram-prod-org-1", name)
	require.Equal(t, "acme (prod environment)", label)

	name, label = upstreamKeyIdentity("prod", "org-1", "acme", KeyTypeInternal)
	require.Equal(t, "gram-prod-org-1-internal", name)
	require.Equal(t, "acme (prod environment, internal)", label)
}

// newKeyTypeTestClient builds a unified client whose completion calls hit a
// minimal mock OpenRouter server, with a recording provisioner swapped in so
// tests can assert which of the org's keys paid for a request.
func newKeyTypeTestClient(t *testing.T) (*ChatClient, *mockProvisioner) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_kt",
			"model": "openai/gpt-5.4",
			"choices": [{"message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2, "cost": 0.0001}
		}`))
	}))
	t.Cleanup(server.Close)

	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	client := newTestClientForServer(t, server)
	client.provisioner = provisioner
	client.keyResolver = &PlatformKeyResolver{Provisioner: provisioner}
	return client, provisioner
}

func TestChatClient_GetCompletion_DefaultKeyTypeIsChat(t *testing.T) {
	t.Parallel()

	client, provisioner := newKeyTypeTestClient(t)

	_, err := client.GetCompletion(t.Context(), CompletionRequest{
		OrgID:       "org-kt",
		ProjectID:   uuid.NewString(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	})
	require.NoError(t, err)

	require.Equal(t, []KeyType{KeyTypeChat}, provisioner.ProvisionedKeyTypes(),
		"an unset key type must provision the chat key")
}

func TestChatClient_GetCompletion_ExplicitInternalKeyType(t *testing.T) {
	t.Parallel()

	client, provisioner := newKeyTypeTestClient(t)

	_, err := client.GetCompletion(t.Context(), CompletionRequest{
		OrgID:       "org-kt",
		ProjectID:   uuid.NewString(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.Nil,
		UsageSource: billing.ModelUsageSourceRiskAnalysis,
		KeyType:     KeyTypeInternal,
	})
	require.NoError(t, err)

	require.Equal(t, []KeyType{KeyTypeInternal}, provisioner.ProvisionedKeyTypes())
}

// TestChatClient_GetCompletion_RiskAnalysisRequiresInternalKey pins the
// pairing guard: risk-analysis inference never legitimately bills the Other inference
// key, so a caller that forgets KeyType must fail fast rather than silently
// drain the customer's Other inference cap.
func TestChatClient_GetCompletion_RiskAnalysisRequiresInternalKey(t *testing.T) {
	t.Parallel()

	client, provisioner := newKeyTypeTestClient(t)

	_, err := client.GetCompletion(t.Context(), CompletionRequest{
		OrgID:       "org-kt",
		ProjectID:   uuid.NewString(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.Nil,
		UsageSource: billing.ModelUsageSourceRiskAnalysis,
	})
	require.ErrorContains(t, err, "requires KeyTypeInternal")
	require.Empty(t, provisioner.ProvisionedKeyTypes(), "no key may be provisioned for a rejected pairing")
}

func TestChatClient_GetObjectCompletion_PropagatesKeyType(t *testing.T) {
	t.Parallel()

	client, provisioner := newKeyTypeTestClient(t)

	_, err := client.GetObjectCompletion(t.Context(), ObjectCompletionRequest{
		OrgID:       "org-kt",
		ProjectID:   uuid.NewString(),
		Model:       "openai/gpt-5.4",
		Prompt:      "judge this",
		UsageSource: billing.ModelUsageSourceRiskAnalysis,
		KeyType:     KeyTypeInternal,
	})
	require.NoError(t, err)

	require.Equal(t, []KeyType{KeyTypeInternal}, provisioner.ProvisionedKeyTypes())
}

func TestChatClient_CreateEmbeddings_UsesSelectedKeyType(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		options  []EmbeddingOption
		expected KeyType
	}{
		{name: "chat by default", options: nil, expected: KeyTypeChat},
		{name: "internal when selected", options: []EmbeddingOption{WithEmbeddingKeyType(KeyTypeInternal)}, expected: KeyTypeInternal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, provisioner := newKeyTypeTestClient(t)

			// The embeddings path calls the vendor SDK against the real base
			// URL, so the call fails after provisioning the selected key.
			_, _ = client.CreateEmbeddings(t.Context(), "org-kt", "openai/text-embedding-3-small", []string{"hello"}, tt.options...)

			require.Equal(t, []KeyType{tt.expected}, provisioner.ProvisionedKeyTypes())
		})
	}
}
