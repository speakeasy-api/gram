package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/environments"
	env_repo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/instances"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
	tools_repo "github.com/speakeasy-api/gram/internal/tools/repo"
	"github.com/speakeasy-api/gram/internal/toolsets"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type ChatClient struct {
	logger     *slog.Logger
	openRouter openrouter.Provisioner
	chatClient *openrouter.ChatClient
	db         *pgxpool.Pool
	enc        *encryption.Encryption
	tracer     trace.Tracer
	metrics    *o11y.MetricsHandler
}

func NewChatClient(logger *slog.Logger, db *pgxpool.Pool, openRouter openrouter.Provisioner, chatClient *openrouter.ChatClient, enc *encryption.Encryption) *ChatClient {
	return &ChatClient{
		logger:     logger,
		openRouter: openRouter,
		chatClient: chatClient,
		db:         db,
		enc:        enc,
		tracer:     otel.Tracer("github.com/speakeasy-api/gram/internal/chat"),
		metrics:    o11y.NewMetricsHandler(),
	}
}

type AgentTool struct {
	Definition openrouter.Tool
	Executor   func(ctx context.Context, rawArgs string) (string, error)
}

type AgentChatOptions struct {
	SystemPrompt            *string
	ToolsetSlug             *string
	AdditionalTools         []AgentTool
	AddedEnvironmentEntries map[string]string
	AgentTimeout            *time.Duration
}

// AgentChat loops over tool calls until completion and returns the final message.
func (c *ChatClient) AgentChat(
	ctx context.Context,
	orgID string,
	projectID uuid.UUID,
	prompt string,
	opts AgentChatOptions,
) (string, error) {
	if opts.AgentTimeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *opts.AgentTimeout)
		defer cancel()
	}

	var messages []openrouter.OpenAIChatMessage

	// Optional system prompt
	if opts.SystemPrompt != nil {
		messages = append(messages, openrouter.OpenAIChatMessage{
			Role:       "system",
			Content:    *opts.SystemPrompt,
			ToolCalls:  nil,
			ToolCallID: "",
			Name:       "",
		})
	}

	// User message
	messages = append(messages, openrouter.OpenAIChatMessage{
		Role:       "user",
		Content:    prompt,
		ToolCalls:  nil,
		ToolCallID: "",
		Name:       "",
	})

	// Register tool definitions and their executors
	agentTools := opts.AdditionalTools
	if opts.ToolsetSlug != nil {
		toolsetTools, err := c.LoadToolsetTools(ctx, projectID, *opts.ToolsetSlug, opts.AddedEnvironmentEntries)
		if err != nil {
			return "", fmt.Errorf("failed to load toolset tools: %w", err)
		}
		agentTools = append(agentTools, toolsetTools...)
	}
	toolDefs := make([]openrouter.Tool, 0, len(agentTools))
	executors := make(map[string]func(context.Context, string) (string, error))
	for _, t := range agentTools {
		if t.Definition.Function != nil {
			toolDefs = append(toolDefs, t.Definition)
			executors[t.Definition.Function.Name] = t.Executor
		}
	}

	for {
		msg, err := c.chatClient.GetCompletionFromMessages(ctx, orgID, messages, toolDefs)
		if err != nil {
			return "", fmt.Errorf("failed to get completion: %w", err)
		}

		messages = append(messages, *msg)

		// No tool calls = final assistant message
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		// Tool call loop
		for _, tc := range msg.ToolCalls {
			c.logger.InfoContext(ctx, "Tool called", slog.String("name", tc.Function.Name), slog.String("args", tc.Function.Arguments))

			exec, ok := executors[tc.Function.Name]
			var output string

			if !ok {
				output = fmt.Sprintf("No executor found for %q", tc.Function.Name)
				c.logger.ErrorContext(ctx, "Missing executor", slog.String("tool", tc.Function.Name))
			} else {
				result, err := exec(ctx, tc.Function.Arguments)
				if err != nil {
					output = fmt.Sprintf("Error calling tool %q: %v", tc.Function.Name, err)
					c.logger.ErrorContext(ctx, "Tool error", slog.String("tool", tc.Function.Name), slog.String("error", err.Error()))
				} else {
					output = result
				}
			}

			messages = append(messages, openrouter.OpenAIChatMessage{
				Role:       "tool",
				Content:    output,
				Name:       tc.Function.Name,
				ToolCallID: tc.ID,
				ToolCalls:  nil,
			})
		}
	}
}

func (c *ChatClient) LoadToolsetTools(
	ctx context.Context,
	projectID uuid.UUID,
	toolsetSlug string,
	addedEnvironmentEntries map[string]string,
) ([]AgentTool, error) {
	toolset, err := mv.DescribeToolset(ctx, c.logger, c.db, mv.ProjectID(projectID), mv.ToolsetSlug(toolsetSlug))
	if err != nil {
		return nil, fmt.Errorf("failed to load toolset: %w", err)
	}

	if toolset.DefaultEnvironmentSlug == nil {
		return nil, fmt.Errorf("toolset has no default environment slug")
	}

	envRepo := env_repo.New(c.db)
	toolsRepo := tools_repo.New(c.db)
	entries := environments.NewEnvironmentEntries(c.logger, envRepo, c.enc)
	toolsetHelpers := toolsets.NewToolsets(c.db)
	envSlug := string(*toolset.DefaultEnvironmentSlug)

	// Find environment by slug
	envModel, err := envRepo.GetEnvironmentBySlug(ctx, env_repo.GetEnvironmentBySlugParams{
		ProjectID: projectID,
		Slug:      strings.ToLower(envSlug),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load environment: %w", err)
	}

	// Load environment entries
	environmentEntries, err := entries.ListEnvironmentEntries(ctx, envModel.ID, false)
	if err != nil {
		return nil, fmt.Errorf("failed to load environment entries: %w", err)
	}

	agentTools := make([]AgentTool, 0, len(toolset.HTTPTools))
	for _, httpTool := range toolset.HTTPTools {
		if httpTool == nil {
			continue
		}

		// Capture for closure
		name := httpTool.Name
		projID := projectID

		executor := func(ctx context.Context, rawArgs string) (string, error) {

			// Find tool by name
			toolID, err := toolsRepo.PokeHTTPToolDefinitionByName(ctx, tools_repo.PokeHTTPToolDefinitionByNameParams{
				ProjectID: projID,
				Name:      name,
			})
			if err != nil {
				return "", fmt.Errorf("failed to load tool: %w", err)
			}
			if toolID == uuid.Nil {
				return "", fmt.Errorf("tool not found")
			}

			executionPlan, err := toolsetHelpers.GetHTTPToolExecutionInfoByID(ctx, toolID, projID)
			if err != nil {
				return "", fmt.Errorf("failed to get tool execution info: %w", err)
			}

			rw := &toolCallResponseWriter{
				headers:    make(http.Header),
				body:       new(bytes.Buffer),
				statusCode: http.StatusOK,
			}

			// Transform environment entries into a map
			envVars := make(map[string]string)
			for _, entry := range environmentEntries {
				envVars[entry.Name] = entry.Value
			}

			// use environment overrides
			for key, value := range addedEnvironmentEntries {
				envVars[key] = value
			}

			err = instances.InstanceToolProxy(ctx, c.tracer, c.logger, c.metrics, rw, bytes.NewBufferString(rawArgs), envVars, executionPlan)
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
			Executor: executor,
		})
	}

	return agentTools, nil
}

type toolCallResponseWriter struct {
	statusCode int
	headers    http.Header
	body       *bytes.Buffer
}

func (w *toolCallResponseWriter) Header() http.Header {
	return w.headers
}

func (w *toolCallResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *toolCallResponseWriter) Write(p []byte) (int, error) {
	return w.body.Write(p)
}

var jsonRE = regexp.MustCompile(`\bjson\b`)
var yamlRE = regexp.MustCompile(`\byaml\b`)

type FormatResult struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Data string `json:"data,omitempty"`
}

func formatResult(rw toolCallResponseWriter) (FormatResult, error) {
	body := rw.body.Bytes()
	if len(body) == 0 {
		return FormatResult{"", "", ""}, nil
	}

	ct := rw.headers.Get("content-type")
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return FormatResult{"", "", ""}, fmt.Errorf("failed to parse content type %q: %w", ct, err)
	}

	switch {
	case strings.HasPrefix(mt, "text/"), jsonRE.MatchString(mt), yamlRE.MatchString(mt):
		return FormatResult{Type: "text", Text: string(body), Data: ""}, nil
	case strings.HasPrefix(mt, "image/"):
		encoded := base64.StdEncoding.EncodeToString(body)
		return FormatResult{Type: "image", Data: encoded, Text: ""}, nil
	case strings.HasPrefix(mt, "audio/"):
		encoded := base64.StdEncoding.EncodeToString(body)
		return FormatResult{Type: "audio", Data: encoded, Text: ""}, nil
	default:
		return FormatResult{"", "", ""}, fmt.Errorf("unsupported content type %q", ct)
	}
}
