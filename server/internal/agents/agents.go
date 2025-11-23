package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	env_repo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	tm "github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

// Request types matching OpenAI Responses API

type ResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ResponseInput any

type Toolset struct {
	ToolsetSlug     string            `json:"toolset_slug"`
	EnvironmentSlug string            `json:"environment_slug"`
	Headers         map[string]string `json:"headers"`
}

type ResponseRequest struct {
	ProjectSlug        string        `json:"project_slug"`
	Model              string        `json:"model"`
	Instructions       *string       `json:"instructions,omitempty"`
	Input              ResponseInput `json:"input"` // Can be string or []ResponseMessage
	PreviousResponseID *string       `json:"previous_response_id,omitempty"`
	Temperature        *float64      `json:"temperature,omitempty"`
	Toolsets           []Toolset     `json:"toolsets,omitempty"`
	Async              *bool         `json:"async,omitempty"`
}

// Response types matching OpenAI Responses API

type OutputTextContent struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text"`
}

// OutputMessage represents an output message from the model
type OutputMessage struct {
	Type    string              `json:"type"` // "message"
	ID      string              `json:"id"`
	Status  string              `json:"status"` // "in_progress", "completed", or "incomplete"
	Role    string              `json:"role"`   // "assistant"
	Content []OutputTextContent `json:"content"`
}

// MCPToolCall represents an MCP tool invocation in OpenAI Responses API format
type MCPToolCall struct {
	Type        string  `json:"type"` // "mcp_call"
	ID          string  `json:"id"`
	ServerLabel string  `json:"server_label"`
	Name        string  `json:"name"`
	Arguments   string  `json:"arguments"`
	Output      string  `json:"output"`
	Error       *string `json:"error,omitempty"`
	Status      string  `json:"status"` // "in_progress", "completed", "incomplete", "calling", or "failed"
}

// OutputItem is a union type for different output types
// Can be one of: OutputMessage, MCPToolCall, FunctionToolCall
type OutputItem any

type ResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type TextFormat struct {
	Type string `json:"type"` // "text"
}

type ResponseText struct {
	Format TextFormat `json:"format"`
}

type Reasoning struct {
	Effort  *string `json:"effort"`
	Summary *string `json:"summary"`
}

type ResponseOutput struct {
	ID                 string        `json:"id"`
	Object             string        `json:"object"` // "response"
	CreatedAt          int64         `json:"created_at"`
	Status             string        `json:"status"` // "completed"
	Error              *string       `json:"error"`
	Instructions       *string       `json:"instructions"`
	Model              string        `json:"model"`
	Output             []OutputItem  `json:"output"` // Can contain OutputMessage, MCPToolCall, or FunctionToolCall
	PreviousResponseID *string       `json:"previous_response_id"`
	Temperature        float64       `json:"temperature"`
	Text               ResponseText  `json:"text"`
	Usage              ResponseUsage `json:"usage"`
}

type AgentTool struct {
	Definition  openrouter.Tool
	Executor    func(ctx context.Context, rawArgs string) (string, error)
	IsMCPTool   bool   // true if this tool comes from a toolset (MCP server)
	ServerLabel string // label of the MCP server (e.g., toolset slug)
}

type AgentChatOptions struct {
	SystemPrompt    *string
	Toolsets        []Toolset
	AdditionalTools []AgentTool
	AgentTimeout    *time.Duration
}

// Service holds dependencies for agent operations
type Service struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	env          *environments.EnvironmentEntries
	toolProxy    *gateway.ToolProxy
	cache        cache.Cache
	toolsetCache cache.TypedCacheObject[mv.ToolsetBaseContents]
	chatClient   *openrouter.ChatClient
}

// NewService creates a new agents service
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	env *environments.EnvironmentEntries,
	enc *encryption.Client,
	cacheImpl cache.Cache,
	guardianPolicy *guardian.Policy,
	funcCaller functions.ToolCaller,
	openRouter openrouter.Provisioner,
	baseChatClient *openrouter.ChatClient,
) *Service {
	logger = logger.With(attr.SlogComponent("agents"))

	return &Service{
		logger: logger,
		db:     db,
		env:    env,
		toolProxy: gateway.NewToolProxy(
			logger,
			tracerProvider,
			meterProvider,
			gateway.ToolCallSourceDirect,
			enc,
			cacheImpl,
			guardianPolicy,
			funcCaller,
		),
		cache:        cacheImpl,
		toolsetCache: cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheImpl, cache.SuffixNone),
		chatClient:   baseChatClient,
	}
}

// toolCallResponseWriter captures the response from a tool call
type toolCallResponseWriter struct {
	headers    http.Header
	body       *bytes.Buffer
	statusCode int
}

func (w *toolCallResponseWriter) Header() http.Header {
	return w.headers
}

func (w *toolCallResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b) //nolint:wrapcheck // passthrough from bytes.Buffer
}

func (w *toolCallResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

// formatResult formats the tool call result
type toolCallResult struct {
	Text string
	Data string
}

func formatResult(rw toolCallResponseWriter) (toolCallResult, error) {
	contentType := rw.Header().Get("Content-Type")
	if contentType == "" {
		return toolCallResult{Text: rw.body.String(), Data: ""}, nil
	}

	if strings.Contains(contentType, "text/plain") {
		return toolCallResult{Text: rw.body.String(), Data: ""}, nil
	}

	if strings.Contains(contentType, "application/json") {
		return toolCallResult{Text: "", Data: rw.body.String()}, nil
	}

	return toolCallResult{Text: rw.body.String(), Data: ""}, nil
}

// LoadToolsetTools loads tools from a toolset and returns them as AgentTools marked as MCP tools
func (s *Service) LoadToolsetTools(
	ctx context.Context,
	projectID uuid.UUID,
	toolsetSlug string,
	environmentSlug string,
	headers map[string]string,
) ([]AgentTool, error) {
	toolset, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(projectID), mv.ToolsetSlug(toolsetSlug), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	envRepo := env_repo.New(s.db)
	toolsetHelpers := toolsets.NewToolsets(s.db)

	// Use provided environment slug, or fall back to default
	envSlug := environmentSlug
	if envSlug == "" {
		if toolset.DefaultEnvironmentSlug == nil {
			return nil, fmt.Errorf("toolset has no default environment slug")
		}
		envSlug = string(*toolset.DefaultEnvironmentSlug)
	}

	// Find environment by slug
	envModel, err := envRepo.GetEnvironmentBySlug(ctx, env_repo.GetEnvironmentBySlugParams{
		ProjectID: projectID,
		Slug:      strings.ToLower(envSlug),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load environment: %w", err)
	}

	// Load environment entries
	environmentEntries, err := s.env.ListEnvironmentEntries(ctx, projectID, envModel.ID, false)
	if err != nil {
		return nil, fmt.Errorf("failed to load environment entries: %w", err)
	}

	agentTools := make([]AgentTool, 0, len(toolset.Tools))
	for _, tool := range toolset.Tools {
		if tool == nil {
			continue
		}

		if tool.HTTPToolDefinition == nil {
			// TODO: support other tool types
			continue
		}

		toolURN, err := conv.GetToolURN(*tool)
		if err != nil {
			return nil, fmt.Errorf("failed to get tool urn: %w", err)
		}

		httpTool := tool.HTTPToolDefinition

		// Capture for closure
		name := httpTool.Name
		projID := projectID

		executor := func(ctx context.Context, rawArgs string) (string, error) {
			plan, err := toolsetHelpers.GetToolCallPlanByURN(ctx, *toolURN, projID)
			if err != nil {
				return "", fmt.Errorf("failed to get tool call plan: %w", err)
			}

			descriptor := plan.Descriptor
			ctx, _ = o11y.EnrichToolCallContext(ctx, s.logger, descriptor.OrganizationSlug, descriptor.ProjectSlug)

			systemConfig, err := s.env.LoadSourceEnv(ctx, projID, string(toolURN.Kind), toolURN.Source)
			if err != nil {
				return "", fmt.Errorf("failed to load system environment: %w", err)
			}

			rw := &toolCallResponseWriter{
				headers:    make(http.Header),
				body:       new(bytes.Buffer),
				statusCode: http.StatusOK,
			}

			ciEnv := gateway.NewCaseInsensitiveEnv()
			for _, entry := range environmentEntries {
				ciEnv.Set(entry.Name, entry.Value)
			}

			// use header overrides
			for key, value := range headers {
				ciEnv.Set(key, value)
			}

			err = s.toolProxy.Do(ctx, rw, bytes.NewBufferString(rawArgs), gateway.ToolCallEnv{
				SystemEnv:  gateway.CIEnvFrom(systemConfig),
				UserConfig: ciEnv,
			}, plan, tm.NewNoopToolCallLogger())
			if err != nil {
				return "", fmt.Errorf("tool proxy error: %w", err)
			}

			result, err := formatResult(*rw)
			if err != nil {
				return "", fmt.Errorf("failed to format tool call result: %w", err)
			}

			if result.Text != "" {
				return result.Text, nil
			}
			if result.Data != "" {
				jsonData, err := json.Marshal(result.Data)
				if err != nil {
					return "", fmt.Errorf("failed to marshal data: %w", err)
				}
				return string(jsonData), nil
			}
			return fmt.Sprintf("status code: %d", rw.statusCode), nil
		}

		schema := json.RawMessage(httpTool.Schema)
		// This check is important for empty schema tool calls
		if httpTool.Schema == "" {
			schema = json.RawMessage(`{}`)
		}

		agentTools = append(agentTools, AgentTool{
			Definition: openrouter.Tool{
				Type: "function",
				Function: &openrouter.FunctionDefinition{
					Name:        name,
					Description: httpTool.Description,
					Parameters:  schema,
				},
			},
			Executor:    executor,
			IsMCPTool:   true,
			ServerLabel: toolsetSlug,
		})
	}

	return agentTools, nil
}

// GetCompletionFromMessages calls the chat client to get a completion from messages
func (s *Service) GetCompletionFromMessages(
	ctx context.Context,
	orgID string,
	messages []openrouter.OpenAIChatMessage,
	toolDefs []openrouter.Tool,
) (*openrouter.OpenAIChatMessage, error) {
	msg, err := s.chatClient.GetCompletionFromMessages(ctx, orgID, messages, toolDefs)
	if err != nil {
		return nil, fmt.Errorf("failed to get completion from messages: %w", err)
	}
	return msg, nil
}
