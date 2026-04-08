package openrouter

import (
	"context"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/billing"
)

// CompletionClient represents any client that can make completion requests to an LLM provider.
// This interface abstracts away the details of HTTP communication, message capture, and usage tracking.
type CompletionClient interface {
	GetCompletion(ctx context.Context, request CompletionRequest) (*CompletionResponse, error)
	GetCompletionStream(ctx context.Context, request CompletionRequest) (StreamReader, error)
	GetObjectCompletion(ctx context.Context, request ObjectCompletionRequest) (*CompletionResponse, error)
	CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string) ([][]float32, error)
}

// StreamReader is an interface for reading streaming completion responses.
type StreamReader interface {
	Read(p []byte) (n int, err error)
	Close() error
}

// HTTPMetadata contains HTTP request metadata for tracking and analytics.
type HTTPMetadata struct {
	Origin    string
	UserAgent string
	IPAddress string
}

// CompletionRequest encapsulates all parameters needed for a completion call.
type CompletionRequest struct {
	// Required fields
	OrgID    string
	Messages []or.Message

	// Optional fields with defaults
	ProjectID   string
	Tools       []Tool
	Temperature *float64
	Model       string
	Stream      bool

	// Context for tracking and capture
	UsageSource    billing.ModelUsageSource
	ChatID         uuid.UUID
	UserID         string
	ExternalUserID string
	UserEmail      string
	HTTPMetadata   *HTTPMetadata
	APIKeyID       string

	JSONSchema *or.JSONSchemaConfig // For structured output mode
}

type ObjectCompletionRequest struct {
	OrgID          string
	ProjectID      string
	Model          string
	SystemPrompt   string
	Prompt         string
	UsageSource    billing.ModelUsageSource
	UserID         string
	ExternalUserID string
	HTTPMetadata   *HTTPMetadata
	JSONSchema     *or.JSONSchemaConfig // For structured output mode
}

// CompletionResponse encapsulates the result of a completion call.
type CompletionResponse struct {
	StartTime    time.Time
	Message      *or.Message
	MessageID    string
	Model        string
	Usage        Usage
	FinishReason *string
	ToolCalls    []ToolCall
	Content      string // Text content extracted from message
}

// MessageCaptureStrategy defines how to capture and persist messages.
// Different implementations can store messages in different ways (database, logs, no-op, etc.).
type MessageCaptureStrategy interface {
	StartOrResumeChat(ctx context.Context, request CompletionRequest) error
	CaptureMessage(ctx context.Context, request CompletionRequest, response CompletionResponse) error
}

// UsageTrackingStrategy defines how to track model usage for billing.
// Different implementations can handle tracking differently (inline, async, with fallback, etc.).
type UsageTrackingStrategy interface {
	TrackUsage(ctx context.Context, generationID, orgID, projectID string, source billing.ModelUsageSource, chatID string) error
}
