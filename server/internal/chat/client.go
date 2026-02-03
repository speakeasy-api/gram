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
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
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
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

type ChatClient struct {
	logger       *slog.Logger
	openRouter   openrouter.Provisioner
	chatClient   *openrouter.ChatClient
	db           *pgxpool.Pool
	env          *environments.EnvironmentEntries
	cache        cache.Cache
	toolProxy    *gateway.ToolProxy
	toolsetCache cache.TypedCacheObject[mv.ToolsetBaseContents]
}

func NewChatClient(logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	openRouter openrouter.Provisioner,
	chatClient *openrouter.ChatClient,
	env *environments.EnvironmentEntries,
	enc *encryption.Client,
	cacheImpl cache.Cache,
	guardianPolicy *guardian.Policy,
	funcCaller functions.ToolCaller,
) *ChatClient {
	return &ChatClient{
		logger:     logger,
		openRouter: openRouter,
		chatClient: chatClient,
		db:         db,
		env:        env,
		cache:      cacheImpl,
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
		toolsetCache: cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheImpl, cache.SuffixNone),
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
	Temperature             *float64
	Model                   string
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

	var messages []or.Message

	// Optional system prompt
	if opts.SystemPrompt != nil {
		messages = append(messages, or.CreateMessageSystem(or.SystemMessage{
			Content: or.CreateSystemMessageContentStr(*opts.SystemPrompt),
			Name:    nil,
		}))
	}

	// User message
	messages = append(messages, or.CreateMessageUser(or.UserMessage{
		Content: or.CreateUserMessageContentStr(prompt),
		Name:    nil,
	}))

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
	toolMap := make(map[string]AgentTool)
	for _, t := range agentTools {
		if t.Definition.Function != nil {
			toolDefs = append(toolDefs, t.Definition)
			toolMap[t.Definition.Function.Name] = t
		}
	}

	for {
		msg, err := c.chatClient.GetCompletionFromMessages(ctx, orgID, projectID.String(), messages, toolDefs, opts.Temperature, opts.Model, billing.ModelUsageSourceAgents)
		if err != nil {
			return "", fmt.Errorf("failed to get completion: %w", err)
		}

		messages = append(messages, *msg)

		// No tool calls = final assistant message
		if msg.Type != or.MessageTypeAssistant {
			return openrouter.GetText(*msg), nil
		}

		// Tool call loop
		for _, tc := range msg.AssistantMessage.ToolCalls {
			c.logger.InfoContext(ctx, "Tool called", attr.SlogToolName(tc.Function.Name))

			var output string
			tool, ok := toolMap[tc.Function.Name]
			if !ok || tool.Executor == nil {
				output = fmt.Sprintf("No tool found for %q", tc.Function.Name)
				c.logger.ErrorContext(ctx, "Missing tool", attr.SlogToolName(tc.Function.Name))
			} else {
				result, err := tool.Executor(ctx, tc.Function.Arguments)
				if err != nil {
					output = fmt.Sprintf("Error calling tool %q: %v", tc.Function.Name, err)
					c.logger.ErrorContext(ctx, "Tool error", attr.SlogToolName(tc.Function.Name), attr.SlogError(err))
				} else {
					output = result
				}
			}

			messages = append(messages, or.CreateMessageTool(or.ToolResponseMessage{
				Content:    or.CreateToolResponseMessageContentStr(output),
				ToolCallID: tc.ID,
			}))
		}
	}
}

func (c *ChatClient) LoadToolsetTools(
	ctx context.Context,
	projectID uuid.UUID,
	toolsetSlug string,
	addedEnvironmentEntries map[string]string,
) ([]AgentTool, error) {
	toolset, err := mv.DescribeToolset(ctx, c.logger, c.db, mv.ProjectID(projectID), mv.ToolsetSlug(toolsetSlug), &c.toolsetCache)
	if err != nil {
		return nil, err
	}

	if toolset.DefaultEnvironmentSlug == nil {
		return nil, fmt.Errorf("toolset has no default environment slug")
	}

	envRepo := env_repo.New(c.db)
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
	environmentEntries, err := c.env.ListEnvironmentEntries(ctx, projectID, envModel.ID, false)
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
		toolsetIDParsed, err := uuid.Parse(toolset.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse toolset ID: %w", err)
		}

		executor := func(ctx context.Context, rawArgs string) (string, error) {
			plan, err := toolsetHelpers.GetToolCallPlanByURN(ctx, *toolURN, projID)
			if err != nil {
				return "", fmt.Errorf("failed to get tool call plan: %w", err)
			}

			descriptor := plan.Descriptor
			ctx, _ = o11y.EnrichToolCallContext(ctx, c.logger, descriptor.OrganizationSlug, descriptor.ProjectSlug)

			systemConfig, err := c.env.LoadSystemEnv(ctx, projID, toolsetIDParsed, string(toolURN.Kind), toolURN.Source)
			if err != nil {
				return "", fmt.Errorf("failed to load system environment: %w", err)
			}

			rw := &toolCallResponseWriter{
				headers:    make(http.Header),
				body:       new(bytes.Buffer),
				statusCode: http.StatusOK,
			}

			ciEnv := toolconfig.NewCaseInsensitiveEnv()
			for _, entry := range environmentEntries {
				ciEnv.Set(entry.Name, entry.Value)
			}

			// use environment overrides
			for key, value := range addedEnvironmentEntries {
				ciEnv.Set(key, value)
			}

			err = c.toolProxy.Do(ctx, rw, bytes.NewBufferString(rawArgs), toolconfig.ToolCallEnv{
				SystemEnv:  systemConfig,
				UserConfig: ciEnv,
				OAuthToken: "", // Chat does not support OAuth tokens for external MCP
			}, plan, tm.HTTPLogAttributes{})
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
	n, err := w.body.Write(p)
	if err != nil {
		return n, fmt.Errorf("write response body error: %w", err)
	}

	return n, nil
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
