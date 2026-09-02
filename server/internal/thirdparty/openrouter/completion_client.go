package openrouter

import (
	"context"
	"encoding/json"
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
	CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string, opts ...EmbeddingOption) ([][]float32, error)
	// KeyResolver reports which OpenRouter key a completion with the given
	// billing coordinates spends; judges use it to scope their shared
	// rate-limit bucket (JudgeRateLimitKey). Resolution may provision a
	// platform key on an org's first use.
	KeyResolver
}

// EmbeddingOption tunes a CreateEmbeddings call. Options are applied in order;
// later options override earlier ones.
type EmbeddingOption func(*EmbeddingOptions)

// EmbeddingOptions is the resolved option set passed to the embedding backend.
// Fields are nil/zero when the corresponding option was not supplied.
type EmbeddingOptions struct {
	// Dimensions requests Matryoshka truncation to N dimensions when set.
	Dimensions *int64
	// KeyType selects the organization key pool used for the request.
	KeyType KeyType
	// InputFallbacks supplies progressively smaller alternatives for each input.
	// The outer index corresponds to inputs; inner values are tried in order.
	InputFallbacks [][]string
}

// WithEmbeddingDimensions requests a specific output dimensionality from the
// embedding model. Providers that support Matryoshka embeddings will truncate
// to N dimensions. Must be positive.
func WithEmbeddingDimensions(dimensions int) EmbeddingOption {
	return func(o *EmbeddingOptions) {
		d := int64(dimensions)
		o.Dimensions = &d
	}
}

// WithEmbeddingKeyType selects the organization key pool used for an
// embedding request. The default is KeyTypeChat.
func WithEmbeddingKeyType(keyType KeyType) EmbeddingOption {
	return func(o *EmbeddingOptions) {
		o.KeyType = keyType
	}
}

// WithEmbeddingInputFallbacks supplies progressively smaller alternatives for
// oversized inputs. The client tries each alternative before truncating the
// final one to the model's token limit.
func WithEmbeddingInputFallbacks(fallbacks [][]string) EmbeddingOption {
	return func(o *EmbeddingOptions) {
		o.InputFallbacks = fallbacks
	}
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
// ToolChoiceNone is the tool_choice that forbids tool calls while keeping
// the tools key defined.
var ToolChoiceNone = json.RawMessage(`"none"`)

type CompletionRequest struct {
	// Required fields
	OrgID    string
	Messages []or.ChatMessages

	// Optional fields with defaults
	ProjectID   string
	Tools       []Tool
	Temperature *float64
	Model       string
	Stream      bool

	// ToolChoice, when set, is forwarded verbatim as OpenAI tool_choice —
	// raw because the spec admits strings and function-naming objects, and
	// the chat proxy forwards whatever the client sent. Internal callers use
	// ToolChoiceNone to forbid further tool calls while keeping Tools
	// defined — the shape Anthropic-family models require once the history
	// contains tool turns. Nil means provider default ("auto" when tools
	// are present).
	ToolChoice json.RawMessage

	// Context for tracking and capture
	UsageSource    billing.ModelUsageSource
	ChatID         uuid.UUID
	UserID         string
	ExternalUserID string
	UserEmail      string
	HTTPMetadata   *HTTPMetadata
	APIKeyID       string

	// KeyType selects which of the org's OpenRouter keys pays for the call;
	// the zero value resolves to the chat key. An explicit request field —
	// never derived from UsageSource, which the completions proxy accepts
	// from clients.
	KeyType KeyType

	// KeySlot selects the customer key slot the call resolves against; the
	// zero value falls back to UsageSource. Callers whose UsageSource is
	// client-claimed (the completions proxy) must set it from the
	// authenticated surface instead, so a claimed source cannot bill another
	// slot's customer key.
	KeySlot billing.ModelUsageSource

	JSONSchema *or.ChatJSONSchemaConfig // For structured output mode

	// Reasoning, when set, is forwarded verbatim to OpenRouter on the
	// outbound `/chat/completions` body. Callers use it to disable or shape
	// reasoning generation on routes (e.g. Anthropic) that would otherwise
	// produce billable reasoning tokens.
	Reasoning *Reasoning

	CacheControl *or.AnthropicCacheControlDirective

	// NormalizeOutboundMessages drops narrative text from assistant messages
	// that also carry tool_calls before forwarding to OpenRouter. Opt-in via
	// the `unstable_normalizeOutboundMessages=1` query string on the proxy.
	NormalizeOutboundMessages bool

	// WebSearch, when set, runs OpenRouter's web-search plugin for the
	// request. Results come back as url_citation annotations on the
	// response. Search carries its own per-result charge on top of the
	// completion tokens.
	WebSearch *WebSearchOptions

	// DisableResponseHealing turns off OpenRouter's response-healing plugin
	// for the request. Healing "repairs" malformed structured output into a
	// schema-valid object by stuffing unparseable content into string fields
	// and inventing literal "placeholder" filler — a caller that validates
	// its output wants the malformed original to fail loudly instead.
	DisableResponseHealing bool
}

// WebSearchOptions configures the web-search plugin for one request.
type WebSearchOptions struct {
	// MaxResults caps how many results the plugin returns; zero leaves the
	// provider default.
	MaxResults int
}

type ObjectCompletionRequest struct {
	OrgID          string
	ProjectID      string
	Model          string
	SystemPrompt   string
	Prompt         string
	Temperature    *float64
	UsageSource    billing.ModelUsageSource
	UserID         string
	ExternalUserID string
	UserEmail      string
	HTTPMetadata   *HTTPMetadata
	JSONSchema     *or.ChatJSONSchemaConfig // For structured output mode
	// KeyType selects which of the org's OpenRouter keys pays for the call;
	// the zero value resolves to the chat key.
	KeyType KeyType
	// KeySlot selects the customer key slot the call resolves against; the
	// zero value falls back to UsageSource.
	KeySlot billing.ModelUsageSource
	// Reasoning overrides the reasoning configuration for this call. Nil
	// disables reasoning, which is what a structured-output judge wants: the
	// answer is a schema-shaped verdict, not an argument, and reasoning tokens
	// are billed. Some routes reject a disabled setting outright ("Reasoning is
	// mandatory for this endpoint"), so a caller that needs such a model must
	// set an effort here rather than silently taking a 400.
	Reasoning *Reasoning

	// DisableResponseHealing turns off OpenRouter's response-healing plugin,
	// so malformed structured output fails the caller's validation instead
	// of being "repaired" into schema-valid placeholder filler.
	DisableResponseHealing bool
}

// CompletionResponse encapsulates the result of a completion call.
type CompletionResponse struct {
	StartTime    time.Time
	Message      *or.ChatMessages
	MessageID    string
	Model        string
	Usage        Usage
	FinishReason *string
	ToolCalls    []ToolCall
	Content      string // Text content extracted from message

	// Annotations carries the response message's annotations — the
	// web-search plugin's url_citation results land here. Nil when the
	// request ran no plugin or the route emitted none.
	Annotations []ResponseAnnotation
}

// CaptureSession carries strategy-specific state between StartOrResumeChat and
// CaptureMessage within a single completion request. It lets the strategy
// avoid repeating work (chain-tip lookups, matching walks) on the happy path
// and catch up atomically on the sad path when upfront persistence failed.
// The concrete type is owned by the strategy — callers treat it as opaque.
type CaptureSession = any

// MessageCaptureStrategy defines how to capture and persist messages.
// Different implementations can store messages in different ways (database, logs, no-op, etc.).
type MessageCaptureStrategy interface {
	StartOrResumeChat(ctx context.Context, request CompletionRequest) (CaptureSession, error)
	CaptureMessage(ctx context.Context, session CaptureSession, request CompletionRequest, response CompletionResponse) error
}

// UsageTrackingStrategy defines how to track model usage for billing.
// Implementations consume the ModelUsage decoded from the inline OpenRouter
// response. Callers pass a nil usage when no accounting payload is available
// (e.g. an aborted stream) and the strategy is expected to no-op.
type UsageTrackingStrategy interface {
	TrackUsage(ctx context.Context, usage *ModelUsage, orgID, projectID string, source billing.ModelUsageSource, chatID string) error
}
