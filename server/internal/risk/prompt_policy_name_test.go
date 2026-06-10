package risk

import (
	"context"
	"errors"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestFallbackPromptPolicyName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		prompt   string
		existing []string
		want     string
	}{
		"from prompt": {
			prompt: "  Block   destructive deletes  ",
			want:   "Block destructive deletes",
		},
		"fallback": {
			prompt: "",
			want:   "Prompt Policy",
		},
		"dedupe": {
			prompt:   "Block destructive deletes",
			existing: []string{"Block destructive deletes"},
			want:     "Block destructive deletes 2",
		},
		"truncates": {
			prompt: "1234567890123456789012345678901234567890123456789012345678901",
			want:   "123456789012345678901234567890123456789012345678901234567890",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := fallbackPromptPolicyName(tt.prompt, tt.existing)
			if got != tt.want {
				t.Fatalf("fallbackPromptPolicyName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGeneratePromptPolicyName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		response *openrouter.CompletionResponse
		err      error
		existing []string
		want     string
	}{
		"uses generated name": {
			response: promptNameResponse("Destructive Delete Guard"),
			want:     "Destructive Delete Guard",
		},
		"dedupes generated name": {
			response: promptNameResponse("Destructive Delete Guard"),
			existing: []string{"Destructive Delete Guard"},
			want:     "Destructive Delete Guard 2",
		},
		"falls back on error": {
			err:  errors.New("completion failed"),
			want: "Block destructive deletes",
		},
		"falls back on empty response": {
			response: promptNameResponse("   "),
			want:     "Block destructive deletes",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := &promptNameCompletionClient{
				response: tt.response,
				err:      tt.err,
			}
			svc := &Service{
				logger:           testenv.NewLogger(t),
				completionClient: client,
			}

			got := svc.generatePromptPolicyName(context.Background(), "org_123", "project_123", "Block destructive deletes", tt.existing)
			if got != tt.want {
				t.Fatalf("generatePromptPolicyName() = %q, want %q", got, tt.want)
			}

			if tt.err == nil && tt.response != nil && len(client.requests) != 1 {
				t.Fatalf("GetCompletion called %d times, want 1", len(client.requests))
			}
		})
	}
}

func TestGeneratePromptPolicyNameWithoutCompletionClient(t *testing.T) {
	t.Parallel()

	svc := &Service{logger: testenv.NewLogger(t)}
	got := svc.generatePromptPolicyName(context.Background(), "org_123", "project_123", "Block destructive deletes", nil)
	if got != "Block destructive deletes" {
		t.Fatalf("generatePromptPolicyName() = %q, want %q", got, "Block destructive deletes")
	}
}

type promptNameCompletionClient struct {
	response *openrouter.CompletionResponse
	err      error
	requests []openrouter.CompletionRequest
}

func (c *promptNameCompletionClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	c.requests = append(c.requests, request)
	return c.response, c.err
}

func (c *promptNameCompletionClient) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, errors.New("not implemented")
}

func (c *promptNameCompletionClient) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *promptNameCompletionClient) CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string, opts ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, errors.New("not implemented")
}

func promptNameResponse(text string) *openrouter.CompletionResponse {
	content := or.CreateChatAssistantMessageContentStr(text)
	msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
	return &openrouter.CompletionResponse{
		StartTime: time.Time{},
		Message:   &msg,
		Model:     "test-model",
		Content:   text,
	}
}
