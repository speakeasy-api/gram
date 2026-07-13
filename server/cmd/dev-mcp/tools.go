package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultAssistantModel = "anthropic/claude-sonnet-5"
	defaultTurnTimeout    = 300 * time.Second
	maxTurnTimeout        = 900 * time.Second
	turnPollInterval      = 2 * time.Second
)

func textResult(raw json.RawMessage) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}
}

// projectInput is embedded by every project-scoped tool input.
type projectInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project slug to scope the call to. Omit to use the organization's only project."`
}

type toolsetRef struct {
	ToolsetSlug     string `json:"toolset_slug" jsonschema:"The toolset slug exposed to the assistant."`
	EnvironmentSlug string `json:"environment_slug,omitempty" jsonschema:"Optional environment slug used when invoking the toolset."`
}

type mcpServerRef struct {
	MCPServerSlug   string `json:"mcp_server_slug" jsonschema:"The MCP server slug attached to the assistant."`
	EnvironmentSlug string `json:"environment_slug,omitempty" jsonschema:"Optional environment slug used when connecting to the MCP server."`
}

type idInput struct {
	projectInput
	ID string `json:"id" jsonschema:"The resource ID (UUID)."`
}

type createAssistantInput struct {
	projectInput
	Name           string         `json:"name" jsonschema:"The assistant name."`
	Model          string         `json:"model,omitempty" jsonschema:"OpenRouter model identifier. Defaults to anthropic/claude-sonnet-5."`
	Instructions   string         `json:"instructions" jsonschema:"System instructions for the assistant."`
	Toolsets       []toolsetRef   `json:"toolsets,omitempty" jsonschema:"Toolsets available to the assistant. Defaults to none."`
	MCPServers     []mcpServerRef `json:"mcp_servers,omitempty" jsonschema:"MCP servers attached directly to the assistant."`
	WarmTTLSeconds *int           `json:"warm_ttl_seconds,omitempty" jsonschema:"Warm runtime TTL in seconds. Zero disables the warm window."`
	MaxConcurrency int            `json:"max_concurrency,omitempty" jsonschema:"Maximum active warm runtimes."`
	Status         string         `json:"status,omitempty" jsonschema:"Initial status: active or paused. Defaults to active."`
}

type updateAssistantInput struct {
	projectInput
	ID             string         `json:"id" jsonschema:"The assistant ID."`
	Name           string         `json:"name,omitempty" jsonschema:"New assistant name."`
	Model          string         `json:"model,omitempty" jsonschema:"New OpenRouter model identifier."`
	Instructions   string         `json:"instructions,omitempty" jsonschema:"New system instructions."`
	Toolsets       []toolsetRef   `json:"toolsets,omitempty" jsonschema:"Replacement toolset list."`
	MCPServers     []mcpServerRef `json:"mcp_servers,omitempty" jsonschema:"Replacement MCP server list."`
	WarmTTLSeconds *int           `json:"warm_ttl_seconds,omitempty" jsonschema:"Warm runtime TTL in seconds. Zero disables the warm window."`
	MaxConcurrency int            `json:"max_concurrency,omitempty" jsonschema:"Maximum active warm runtimes."`
	Status         string         `json:"status,omitempty" jsonschema:"New status: active or paused."`
}

type runTurnInput struct {
	projectInput
	AssistantID    string `json:"assistant_id" jsonschema:"The assistant to send the message to."`
	Message        string `json:"message" jsonschema:"The user message text."`
	ChatID         string `json:"chat_id,omitempty" jsonschema:"Existing chat to continue. Omit to start a new conversation."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"How long to wait for the reply before giving up. Defaults to 300, capped at 900."`
}

type loadChatInput struct {
	projectInput
	ChatID     string `json:"chat_id" jsonschema:"The chat ID to load."`
	Generation int    `json:"generation,omitempty" jsonschema:"Generation to load; omit for the latest."`
	FromStart  bool   `json:"from_start,omitempty" jsonschema:"Return the oldest page instead of the newest."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum messages per page (1-200)."`
}

type listChatsInput struct {
	projectInput
	AssistantID string `json:"assistant_id,omitempty" jsonschema:"Filter to chats produced by this assistant."`
}

type createTriggerInput struct {
	projectInput
	DefinitionSlug string         `json:"definition_slug" jsonschema:"Trigger definition slug, e.g. cron. Use list_trigger_definitions to discover the options and their config schemas."`
	Name           string         `json:"name" jsonschema:"The trigger instance name."`
	AssistantID    string         `json:"assistant_id" jsonschema:"The assistant the trigger dispatches to."`
	TargetDisplay  string         `json:"target_display,omitempty" jsonschema:"User-facing target label. Defaults to the assistant ID."`
	Config         map[string]any `json:"config" jsonschema:"Definition-specific config payload, e.g. {\"schedule\": \"*/5 * * * *\"} for cron."`
	Status         string         `json:"status,omitempty" jsonschema:"Initial status."`
}

// chatMessage is the subset of the chat service's message shape run_turn
// needs for terminal detection and reporting.
type chatMessage struct {
	ID  string `json:"id"`
	Seq int64  `json:"seq"`
	// Role and Content mirror the chat service's message shape; Content is
	// usually a JSON string but can be an array of structured parts.
	Content      json.RawMessage `json:"content"`
	Role         string          `json:"role"`
	ToolCalls    *string         `json:"tool_calls"`
	FinishReason *string         `json:"finish_reason"`
	CreatedAt    string          `json:"created_at"`
}

// contentText renders a message's content for the turn result: plain text
// when the content is a JSON string, the raw JSON otherwise.
func contentText(content json.RawMessage) string {
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	return string(content)
}

type chatPage struct {
	Messages      []chatMessage `json:"messages"`
	Generation    int           `json:"generation"`
	MaxGeneration int           `json:"max_generation"`
}

func registerTools(server *mcp.Server, api *apiClient) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "whoami",
		Description: "Show the authenticated local user and their organizations/projects. Use this to discover project slugs.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		raw, err := api.call(ctx, http.MethodGet, "/rpc/auth.info", nil, "", nil)
		if err != nil {
			return nil, nil, err
		}
		var info map[string]any
		if err := json.Unmarshal(raw, &info); err != nil {
			return nil, nil, fmt.Errorf("decode auth info: %w", err)
		}
		delete(info, "session_token")
		delete(info, "session_cookie")
		out, err := json.Marshal(info)
		if err != nil {
			return nil, nil, fmt.Errorf("encode auth info: %w", err)
		}
		return textResult(out), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_assistants",
		Description: "List assistants in the project.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, any, error) {
		raw, err := api.call(ctx, http.MethodGet, "/rpc/assistants.list", nil, in.Project, nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_assistant",
		Description: "Get an assistant by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		raw, err := api.call(ctx, http.MethodGet, "/rpc/assistants.get", url.Values{"id": {in.ID}}, in.Project, nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_assistant",
		Description: "Create an assistant. Toolsets and MCP servers are optional; a bare assistant with instructions is enough to exercise the runtime.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createAssistantInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"name":         in.Name,
			"model":        in.Model,
			"instructions": in.Instructions,
			"toolsets":     in.Toolsets,
		}
		if in.Model == "" {
			body["model"] = defaultAssistantModel
		}
		if in.Toolsets == nil {
			body["toolsets"] = []toolsetRef{}
		}
		if in.MCPServers != nil {
			body["mcp_servers"] = in.MCPServers
		}
		if in.WarmTTLSeconds != nil {
			body["warm_ttl_seconds"] = *in.WarmTTLSeconds
		}
		if in.MaxConcurrency > 0 {
			body["max_concurrency"] = in.MaxConcurrency
		}
		if in.Status != "" {
			body["status"] = in.Status
		}
		raw, err := api.call(ctx, http.MethodPost, "/rpc/assistants.create", nil, in.Project, body)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_assistant",
		Description: "Update an assistant. Only the provided fields change.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateAssistantInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"id": in.ID}
		if in.Name != "" {
			body["name"] = in.Name
		}
		if in.Model != "" {
			body["model"] = in.Model
		}
		if in.Instructions != "" {
			body["instructions"] = in.Instructions
		}
		if in.Toolsets != nil {
			body["toolsets"] = in.Toolsets
		}
		if in.MCPServers != nil {
			body["mcp_servers"] = in.MCPServers
		}
		if in.WarmTTLSeconds != nil {
			body["warm_ttl_seconds"] = *in.WarmTTLSeconds
		}
		if in.MaxConcurrency > 0 {
			body["max_concurrency"] = in.MaxConcurrency
		}
		if in.Status != "" {
			body["status"] = in.Status
		}
		raw, err := api.call(ctx, http.MethodPost, "/rpc/assistants.update", nil, in.Project, body)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_assistant",
		Description: "Delete an assistant by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		if _, err := api.call(ctx, http.MethodDelete, "/rpc/assistants.delete", url.Values{"id": {in.ID}}, in.Project, nil); err != nil {
			return nil, nil, err
		}
		return textResult(json.RawMessage(`{"deleted": true}`)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_turn",
		Description: "Send a message to an assistant and wait for the reply. Starts a new chat unless chat_id is given. Returns the chat ID, the assistant's final reply, and every message the turn produced (tool calls included). This drives the full runtime path: trigger ingest, Temporal, and the runtime container.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in runTurnInput) (*mcp.CallToolResult, any, error) {
		out, err := runTurn(ctx, api, in)
		if err != nil {
			return nil, nil, err
		}
		return textResult(out), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "load_chat",
		Description: "Load a chat transcript page. Useful for inspecting a turn's tool calls or polling a chat manually.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in loadChatInput) (*mcp.CallToolResult, any, error) {
		q := url.Values{"id": {in.ChatID}}
		if in.Generation > 0 {
			q.Set("generation", strconv.Itoa(in.Generation))
		}
		if in.FromStart {
			q.Set("from_start", "true")
		}
		if in.Limit > 0 {
			q.Set("limit", strconv.Itoa(in.Limit))
		}
		raw, err := api.call(ctx, http.MethodGet, "/rpc/chat.load", q, in.Project, nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_chats",
		Description: "List chats in the project, optionally filtered to one assistant's threads.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listChatsInput) (*mcp.CallToolResult, any, error) {
		q := url.Values{}
		if in.AssistantID != "" {
			q.Set("assistant_id", in.AssistantID)
		}
		raw, err := api.call(ctx, http.MethodGet, "/rpc/chat.list", q, in.Project, nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_trigger_definitions",
		Description: "List available trigger definitions and their config schemas.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, any, error) {
		raw, err := api.call(ctx, http.MethodGet, "/rpc/triggers.listDefinitions", nil, in.Project, nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_triggers",
		Description: "List trigger instances in the project.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, any, error) {
		raw, err := api.call(ctx, http.MethodGet, "/rpc/triggers.list", nil, in.Project, nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_trigger",
		Description: "Create a trigger instance that dispatches to an assistant. For quick end-to-end runtime tests, a cron trigger with a tight schedule works well; pause or delete it when done.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTriggerInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"definition_slug": in.DefinitionSlug,
			"name":            in.Name,
			"target_kind":     "assistant",
			"target_ref":      in.AssistantID,
			"target_display":  in.TargetDisplay,
			"config":          in.Config,
		}
		if in.TargetDisplay == "" {
			body["target_display"] = in.AssistantID
		}
		if in.Status != "" {
			body["status"] = in.Status
		}
		raw, err := api.call(ctx, http.MethodPost, "/rpc/triggers.create", nil, in.Project, body)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_trigger",
		Description: "Delete a trigger instance by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		if _, err := api.call(ctx, http.MethodDelete, "/rpc/triggers.delete", url.Values{"id": {in.ID}}, in.Project, nil); err != nil {
			return nil, nil, err
		}
		return textResult(json.RawMessage(`{"deleted": true}`)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "pause_trigger",
		Description: "Pause a trigger instance by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		raw, err := api.call(ctx, http.MethodPost, "/rpc/triggers.pause", url.Values{"id": {in.ID}}, in.Project, nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resume_trigger",
		Description: "Resume a paused trigger instance by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		raw, err := api.call(ctx, http.MethodPost, "/rpc/triggers.resume", url.Values{"id": {in.ID}}, in.Project, nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})
}

func loadChatPage(ctx context.Context, api *apiClient, project, chatID string) (chatPage, error) {
	raw, err := api.call(ctx, http.MethodGet, "/rpc/chat.load", url.Values{"id": {chatID}}, project, nil)
	if err != nil {
		return chatPage{}, err
	}
	var page chatPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return chatPage{}, fmt.Errorf("decode chat page: %w", err)
	}
	return page, nil
}

// collectMessagesAfter pages forward through the chat with `after_seq` keyset
// cursors so a turn that produced more than one page is reported in full.
func collectMessagesAfter(ctx context.Context, api *apiClient, project, chatID string, baselineSeq int64) ([]chatMessage, error) {
	const pageLimit = 200
	var out []chatMessage
	cursor := baselineSeq
	for {
		q := url.Values{"id": {chatID}, "limit": {strconv.Itoa(pageLimit)}}
		if cursor > 0 {
			q.Set("after_seq", strconv.FormatInt(cursor, 10))
		} else {
			q.Set("from_start", "true")
		}
		raw, err := api.call(ctx, http.MethodGet, "/rpc/chat.load", q, project, nil)
		if err != nil {
			return nil, err
		}
		var page chatPage
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode chat page: %w", err)
		}
		if len(page.Messages) == 0 {
			return out, nil
		}
		out = append(out, page.Messages...)
		cursor = page.Messages[len(page.Messages)-1].Seq
		if len(page.Messages) < pageLimit {
			return out, nil
		}
	}
}

func emptyToolCalls(tc *string) bool {
	return tc == nil || *tc == "" || *tc == "[]" || *tc == "null"
}

func runTurn(ctx context.Context, api *apiClient, in runTurnInput) (json.RawMessage, error) {
	timeout := defaultTurnTimeout
	if in.TimeoutSeconds > 0 {
		timeout = min(time.Duration(in.TimeoutSeconds)*time.Second, maxTurnTimeout)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Baseline the newest message so only this turn's output is reported.
	var baselineSeq int64
	if in.ChatID != "" {
		page, err := loadChatPage(ctx, api, in.Project, in.ChatID)
		if err != nil {
			return nil, fmt.Errorf("baseline chat before send: %w", err)
		}
		for _, msg := range page.Messages {
			baselineSeq = max(baselineSeq, msg.Seq)
		}
	}

	sendBody := map[string]any{
		"assistant_id": in.AssistantID,
		"message":      in.Message,
	}
	if in.ChatID != "" {
		sendBody["chat_id"] = in.ChatID
	}
	raw, err := api.call(ctx, http.MethodPost, "/rpc/assistants.sendMessage", nil, in.Project, sendBody)
	if err != nil {
		return nil, err
	}
	var sent struct {
		ChatID   string `json:"chat_id"`
		Accepted bool   `json:"accepted"`
	}
	if err := json.Unmarshal(raw, &sent); err != nil {
		return nil, fmt.Errorf("decode sendMessage result: %w", err)
	}
	if !sent.Accepted {
		return nil, fmt.Errorf("message was not accepted for chat %s", sent.ChatID)
	}

	ticker := time.NewTicker(turnPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for reply on chat %s (the turn may still complete; poll with load_chat): %w", sent.ChatID, ctx.Err())
		case <-ticker.C:
		}

		page, err := loadChatPage(ctx, api, in.Project, sent.ChatID)
		if err != nil {
			return nil, fmt.Errorf("poll chat: %w", err)
		}
		if len(page.Messages) == 0 {
			continue
		}

		last := page.Messages[len(page.Messages)-1]
		terminal := last.Seq > baselineSeq &&
			last.Role == "assistant" &&
			last.FinishReason != nil && *last.FinishReason == "stop" &&
			emptyToolCalls(last.ToolCalls)
		if !terminal {
			continue
		}

		newMessages, err := collectMessagesAfter(ctx, api, in.Project, sent.ChatID, baselineSeq)
		if err != nil {
			return nil, fmt.Errorf("collect turn messages: %w", err)
		}
		reply := contentText(last.Content)
		out, err := json.Marshal(map[string]any{
			"chat_id":      sent.ChatID,
			"reply":        reply,
			"new_messages": newMessages,
		})
		if err != nil {
			return nil, fmt.Errorf("encode turn result: %w", err)
		}
		return out, nil
	}
}
