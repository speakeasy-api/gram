// Package aiinsights serves the built-in `ai-insights` MCP server at
// /mcp/ai-insights. It is a first-party Gram surface that wraps six of the
// eleven /rpc/insights.* endpoints as MCP tools so the chat agent can propose
// edits, write workspace memory, and record findings without going through
// the customer-facing toolset pipeline.
//
// Only the propose/remember/recall/forget/record side of the insights service
// is exposed here. Apply, rollback, dismiss, and forgetMemoryById are
// human-only and remain accessible exclusively via the management API.
package aiinsights

import (
	"context"
	"encoding/json"

	insightsgen "github.com/speakeasy-api/gram/server/gen/insights"
	"github.com/speakeasy-api/gram/server/internal/insights"
)

// Tool is a single MCP tool served by the ai-insights MCP server. The
// dispatch closure receives the raw JSON arguments from tools/call, decodes
// them into the appropriate insights-service payload, and returns the
// insights result serialized as JSON (to be wrapped in MCP text content).
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Dispatch    func(ctx context.Context, svc *insights.Service, args json.RawMessage) (json.RawMessage, error)
}

// proposeVariationArgs mirrors the JSON wire shape of the MCP tool call for
// insights_propose_variation. It is a narrower surface than the full
// ProposeToolVariationPayload — session/apikey/project-slug fields are
// populated by the auth middleware on the HTTP layer, not by the agent.
type proposeVariationArgs struct {
	ToolName      string  `json:"tool_name"`
	ProposedValue any     `json:"proposed_value"`
	CurrentValue  any     `json:"current_value,omitempty"`
	Reasoning     *string `json:"reasoning,omitempty"`
	SourceChatID  *string `json:"source_chat_id,omitempty"`
}

type proposeToolsetChangeArgs struct {
	ToolsetSlug   string  `json:"toolset_slug"`
	ProposedValue any     `json:"proposed_value"`
	CurrentValue  any     `json:"current_value,omitempty"`
	Reasoning     *string `json:"reasoning,omitempty"`
	SourceChatID  *string `json:"source_chat_id,omitempty"`
}

type rememberArgs struct {
	Kind         string   `json:"kind"`
	Content      string   `json:"content"`
	Tags         []string `json:"tags,omitempty"`
	SourceChatID *string  `json:"source_chat_id,omitempty"`
}

type forgetArgs struct {
	MemoryID string `json:"memory_id"`
}

type recallArgs struct {
	Query string   `json:"query,omitempty"`
	Kind  *string  `json:"kind,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	Limit *int     `json:"limit,omitempty"`
}

type recordFindingArgs struct {
	Content      string   `json:"content"`
	Tags         []string `json:"tags,omitempty"`
	SourceChatID *string  `json:"source_chat_id,omitempty"`
}

// Tools returns the six MCP tools exposed by the ai-insights MCP server. The
// descriptions are deliberately opinionated about WHEN to use each tool —
// this is where most of the agentic behavior comes from.
func Tools() []Tool {
	return []Tool{
		{
			Name: "insights_propose_variation",
			Description: "Propose an edit to a tool variation (description, summary, hint, name, confirm mode). " +
				"Use this when log analysis, chat transcripts, or user complaints show the current tool description is misleading users or the tool is behaving in a way a clearer override would fix. " +
				"You must supply `tool_name` and `proposed_value` (a JSON object shaped like an upsert form — at minimum `src_tool_urn` plus any override fields). " +
				"The human reviews the diff in the Insights sidebar and clicks Apply. You cannot apply proposals yourself.",
			InputSchema: mustRawJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tool_name":      map[string]any{"type": "string", "description": "The source tool name the variation targets."},
					"proposed_value": map[string]any{"type": "object", "description": "JSON object shaped like an upsert form: include src_tool_urn plus any override fields (description, summary, name, confirm, hint flags…)."},
					"current_value":  map[string]any{"type": "object", "description": "Optional snapshot of the tool's current variation state; the server will live-read if omitted."},
					"reasoning":      map[string]any{"type": "string", "description": "One or two sentences on why this edit helps. Shown to the human reviewer."},
					"source_chat_id": map[string]any{"type": "string", "description": "Optional chat ID that produced this proposal."},
				},
				"required":             []string{"tool_name", "proposed_value"},
				"additionalProperties": false,
			}),
			Dispatch: dispatchProposeVariation,
		},
		{
			Name: "insights_propose_toolset_change",
			Description: "Propose a change to a toolset composition: add tools, remove tools, or rename it. " +
				"Use this when you notice a tool is missing that users need, or a tool in the set is causing confusion and should be removed. " +
				"Scope is intentionally narrow: add/remove/rename only, no reorder. The human reviews before any change is applied.",
			InputSchema: mustRawJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"toolset_slug":   map[string]any{"type": "string", "description": "The slug of the toolset to change."},
					"proposed_value": map[string]any{"type": "object", "description": "JSON with any of: slug, name, tool_urns (full replacement list), resource_urns, prompt_template_names."},
					"current_value":  map[string]any{"type": "object", "description": "Optional snapshot; the server will live-read if omitted."},
					"reasoning":      map[string]any{"type": "string", "description": "Why this change helps. Shown to the human reviewer."},
					"source_chat_id": map[string]any{"type": "string", "description": "Optional chat ID that produced this proposal."},
				},
				"required":             []string{"toolset_slug", "proposed_value"},
				"additionalProperties": false,
			}),
			Dispatch: dispatchProposeToolsetChange,
		},
		{
			Name: "insights_remember",
			Description: "Write a durable workspace memory. Use this for facts, playbooks, or glossary entries that future sessions should know. " +
				"Prefer short, one-line entries. Tag aggressively — tags drive recall. `kind` must be one of: `fact`, `playbook`, `glossary` (use `insights_record_finding` for short-lived investigation notes instead). " +
				"Memories persist per-project across sessions.",
			InputSchema: mustRawJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":           map[string]any{"type": "string", "enum": []string{"fact", "playbook", "glossary"}, "description": "fact: a discovered truth. playbook: a how-to. glossary: project-specific terminology."},
					"content":        map[string]any{"type": "string", "maxLength": 2000, "description": "The memory content. Keep it short — one or two lines."},
					"tags":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags used for recall ranking. Use domain-ish keywords (customer name, feature area, tool name)."},
					"source_chat_id": map[string]any{"type": "string", "description": "Optional chat ID that produced the memory."},
				},
				"required":             []string{"kind", "content"},
				"additionalProperties": false,
			}),
			Dispatch: dispatchRemember,
		},
		{
			Name: "insights_forget",
			Description: "Delete a workspace memory you wrote earlier. Use this when you later discover a memory was wrong or is no longer relevant. " +
				"Only deletes; the audit log keeps a trail.",
			InputSchema: mustRawJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"memory_id": map[string]any{"type": "string", "description": "The UUID of the memory to delete."},
				},
				"required":             []string{"memory_id"},
				"additionalProperties": false,
			}),
			Dispatch: dispatchForget,
		},
		{
			Name: "insights_recall_memory",
			Description: "Recall workspace memories ranked by tag overlap and recency. " +
				"Call this at the start of an investigation or when a question mentions a topic you might have notes on. " +
				"Filter by `kind` and `tags`. Results bump a usefulness counter that informs pruning.",
			InputSchema: mustRawJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Free-text query describing what you're looking for. Currently advisory — ranking uses tags + recency, not embeddings."},
					"kind":  map[string]any{"type": "string", "enum": []string{"fact", "playbook", "glossary", "finding"}, "description": "Optional kind filter."},
					"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags to rank by; more overlap ranks higher."},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "description": "Max results (default 50)."},
				},
				"additionalProperties": false,
			}),
			Dispatch: dispatchRecall,
		},
		{
			Name: "insights_record_finding",
			Description: "Record a one-line finding during an investigation. " +
				"Use this to track what you've learned mid-investigation so you don't lose state between tool calls. " +
				"Findings auto-expire after 7 days. If something turns out to be durably useful, rewrite it as a `fact` or `playbook` via `insights_remember`.",
			InputSchema: mustRawJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content":        map[string]any{"type": "string", "maxLength": 2000, "description": "The finding. One line is ideal."},
					"tags":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags for later recall."},
					"source_chat_id": map[string]any{"type": "string", "description": "Optional chat ID."},
				},
				"required":             []string{"content"},
				"additionalProperties": false,
			}),
			Dispatch: dispatchRecordFinding,
		},
	}
}

// mustRawJSON marshals v to JSON or panics. Only used at package init time
// for the static tool schemas.
func mustRawJSON(v any) json.RawMessage {
	bs, err := json.Marshal(v)
	if err != nil {
		panic("aiinsights: failed to marshal static tool schema: " + err.Error())
	}
	return bs
}

// ---- Dispatch helpers ----

func dispatchProposeVariation(ctx context.Context, svc *insights.Service, raw json.RawMessage) (json.RawMessage, error) {
	var args proposeVariationArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}

	proposedStr, err := jsonStringify(args.ProposedValue)
	if err != nil {
		return nil, err
	}
	var currentPtr *string
	if args.CurrentValue != nil {
		s, err := jsonStringify(args.CurrentValue)
		if err != nil {
			return nil, err
		}
		currentPtr = &s
	}

	res, err := svc.ProposeToolVariation(ctx, &insightsgen.ProposeToolVariationPayload{
		ToolName:      args.ToolName,
		ProposedValue: proposedStr,
		CurrentValue:  currentPtr,
		Reasoning:     args.Reasoning,
		SourceChatID:  args.SourceChatID,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

func dispatchProposeToolsetChange(ctx context.Context, svc *insights.Service, raw json.RawMessage) (json.RawMessage, error) {
	var args proposeToolsetChangeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}

	proposedStr, err := jsonStringify(args.ProposedValue)
	if err != nil {
		return nil, err
	}
	var currentPtr *string
	if args.CurrentValue != nil {
		s, err := jsonStringify(args.CurrentValue)
		if err != nil {
			return nil, err
		}
		currentPtr = &s
	}

	res, err := svc.ProposeToolsetChange(ctx, &insightsgen.ProposeToolsetChangePayload{
		ToolsetSlug:   args.ToolsetSlug,
		ProposedValue: proposedStr,
		CurrentValue:  currentPtr,
		Reasoning:     args.Reasoning,
		SourceChatID:  args.SourceChatID,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

func dispatchRemember(ctx context.Context, svc *insights.Service, raw json.RawMessage) (json.RawMessage, error) {
	var args rememberArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	res, err := svc.RememberFact(ctx, &insightsgen.RememberFactPayload{
		Kind:         args.Kind,
		Content:      args.Content,
		Tags:         args.Tags,
		SourceChatID: args.SourceChatID,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

func dispatchForget(ctx context.Context, svc *insights.Service, raw json.RawMessage) (json.RawMessage, error) {
	var args forgetArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	res, err := svc.ForgetMemory(ctx, &insightsgen.ForgetMemoryPayload{MemoryID: args.MemoryID})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

func dispatchRecall(ctx context.Context, svc *insights.Service, raw json.RawMessage) (json.RawMessage, error) {
	var args recallArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	res, err := svc.ListMemories(ctx, &insightsgen.ListMemoriesPayload{
		Kind:  args.Kind,
		Tags:  args.Tags,
		Limit: args.Limit,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

func dispatchRecordFinding(ctx context.Context, svc *insights.Service, raw json.RawMessage) (json.RawMessage, error) {
	var args recordFindingArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	res, err := svc.RecordFinding(ctx, &insightsgen.RecordFindingPayload{
		Content:      args.Content,
		Tags:         args.Tags,
		SourceChatID: args.SourceChatID,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

// jsonStringify serializes a decoded JSON value back to its string form for
// endpoints that take JSON-as-string. If the source is already a string we
// validate+pass through (the agent may have already stringified it).
func jsonStringify(v any) (string, error) {
	if s, ok := v.(string); ok {
		if json.Valid([]byte(s)) {
			return s, nil
		}
	}
	bs, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(bs), nil
}
