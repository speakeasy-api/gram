package toolsets

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("chat", func() {
	Description("Managed chats for gram AI consumers.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ChatSessionsToken)

	shared.DeclareErrorResponses()

	Method("listChats", func() {
		Description("List all chats for a project")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("search", String, "Search query (searches chat ID, user ID, and title)")
			Attribute("external_user_id", String, "Filter by external user ID")
			Attribute("source", String, "Filter by agent source. Comma-separated list of exact source values (e.g. 'claude-code,Codex,playground') matched against each session's inferred source; empty for no filter. Use chat.listSources to discover the available values.")
			Attribute("assistant_id", String, "Filter to chats produced by this assistant", func() {
				Format(FormatUUID)
			})
			Attribute("has_risk", String, "Filter by whether chat has risk findings: 'true', 'false', or empty for no filter.", func() {
				Enum("", "true", "false")
			})
			Attribute("account_type", String, "Filter by AI account type: 'team', 'personal', or empty for no filter.", func() {
				Enum("", "team", "personal")
			})
			Attribute("pinned", String, "Filter by pinned state: 'true' for pinned chats, 'false' for unpinned, or empty for no filter.", func() {
				Enum("", "true", "false")
			})
			Attribute("min_risk_score", Int, "Filter to chats with at least this many active risk findings (inclusive). Omit or pass 0 for no threshold.", func() {
				Minimum(0)
			})
			Attribute("from", String, "Filter chats last active after this timestamp (ISO 8601)", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Filter chats last active before this timestamp (ISO 8601)", func() {
				Format(FormatDateTime)
			})
			Attribute("limit", Int, "Number of results per page", func() {
				Default(50)
				Minimum(1)
				Maximum(100)
			})
			Attribute("offset", Int, "Pagination offset", func() {
				Default(0)
				Minimum(0)
			})
			Attribute("sort_by", String, "Field to sort by", func() {
				Enum("last_message_timestamp", "num_messages")
				Default("last_message_timestamp")
			})
			Attribute("sort_order", String, "Sort order", func() {
				Enum("asc", "desc")
				Default("desc")
			})
		})

		Result(ListChatsResult)

		HTTP(func() {
			GET("/rpc/chat.list")
			Param("search")
			Param("external_user_id")
			Param("source")
			Param("assistant_id")
			Param("has_risk")
			Param("account_type")
			Param("pinned")
			Param("min_risk_score")
			Param("from")
			Param("to")
			Param("limit")
			Param("offset")
			Param("sort_by")
			Param("sort_order")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listChats")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListChats", "type": "query"}`)
	})

	Method("loadChat", func() {
		Description("Load a chat by its ID. Messages within a generation are paginated by `seq` keyset: omit cursors to receive the newest page, pass `before_seq` to load older messages (scroll up) or `after_seq` to load newer ones (scroll down). Set `from_start` to receive the oldest page (the start of the thread) instead of the newest. Omit `generation` to receive the latest generation. Set `risk_only` to return only messages with risk findings plus a few messages of surrounding context per finding. Set `query` to instead return only messages whose text matches a search query plus surrounding context (mutually exclusive with `risk_only`).")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("id", String, "The ID of the chat")
			Attribute("generation", Int, "Generation to load. A generation is an immutable snapshot of the chat transcript: a new one is opened whenever the conversation is compacted or an earlier message is edited, while normal turns append to the current generation. Generations are numbered from 0 (oldest) up to `max_generation` (latest). Omit this attribute to receive the latest generation, or page through history by walking from `max_generation` down to 0.", func() {
				Minimum(0)
			})
			Attribute("limit", Int, "Maximum number of messages to return for this page.", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("before_seq", Int64, "Keyset cursor: return the page of messages with `seq` strictly less than this value (older messages). The returned `messages` are always ordered oldest to newest by `seq`, like every other response. Use the `seq` of the oldest message you currently hold to load the previous page. Ignored when `risk_only` is set. Mutually exclusive with `after_seq`; if both are supplied, `after_seq` takes precedence.", func() {
				Minimum(1)
			})
			Attribute("after_seq", Int64, "Keyset cursor: return the page of messages with `seq` strictly greater than this value (newer messages). The returned `messages` are always ordered oldest to newest by `seq`. Use the `seq` of the newest message you currently hold to load the next page. Ignored when `risk_only` is set. Mutually exclusive with `before_seq`; if both are supplied, `after_seq` takes precedence.", func() {
				Minimum(1)
			})
			Attribute("from_start", Boolean, "When true, return the oldest page of the generation (the start of the thread), ordered oldest to newest, with `has_more_before=false`. Page forward from there with `after_seq`. Ignored when `before_seq`, `after_seq`, or `risk_only` is set.", func() {
				Default(false)
			})
			Attribute("risk_only", Boolean, "When true, return only messages that have active risk findings, each padded with a fixed window of surrounding messages, grouped into contiguous segments (see `risk_segments`). The flagged messages themselves are marked with `is_risk` on each `ChatMessage` (surrounding context is `is_risk: false`). Cursors are ignored in this mode; expand a segment with a follow-up `before_seq`/`after_seq` request. Mutually exclusive with `query`.", func() {
				Default(false)
			})
			Attribute("query", String, "When set (and `risk_only` is false), return only messages whose text matches this query (case-insensitive substring over message text, tool names/arguments, and structured content), each padded with a fixed window of surrounding messages, grouped into contiguous segments (see `match_segments`). Cursors are ignored on the initial request; expand a segment with a follow-up `before_seq`/`after_seq` request. Mutually exclusive with `risk_only`.", func() {
				MinLength(1)
				MaxLength(200)
			})
			Required("id")
		})

		Result(Chat)

		HTTP(func() {
			GET("/rpc/chat.load")
			Param("id")
			Param("generation")
			Param("limit")
			Param("before_seq")
			Param("after_seq")
			Param("from_start")
			Param("risk_only")
			Param("query")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "loadChat")
		Meta("openapi:extension:x-speakeasy-name-override", "load")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "LoadChat"}`)
	})

	Method("generateTitle", func() {
		Description("Read or set a chat's title. Omit `title` to return the current/auto-generated title (titles are generated asynchronously after a completion). Provide `title` to set a manual title that auto-generation will never overwrite; provide an empty `title` to clear the manual title and re-enable auto-generation.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("id", String, "The ID of the chat")
			Attribute("title", String, "When present, sets the chat's title manually (empty string resets to auto-generated). When omitted, the current title is returned without changes.", func() {
				MaxLength(200)
			})
			Required("id")
		})

		Result(func() {
			Attribute("title", String, "The current title after the operation (empty when reset to auto-generated)")
			Required("title")
		})

		HTTP(func() {
			POST("/rpc/chat.generateTitle")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "generateTitle")
		Meta("openapi:extension:x-speakeasy-name-override", "generateTitle")
	})

	Method("creditUsage", func() {
		// credit usage is counted at the organization level, no project slug is required.
		Security(security.Session)

		Description("Get the total number of chat credits and usage for the current billing period")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Attribute("credits_used", Float64, "The number of credits remaining")
			Attribute("monthly_credits", Int, "The number of monthly credits")
			Required("credits_used", "monthly_credits")
		})

		HTTP(func() {
			GET("/rpc/chat.creditUsage")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "creditUsage")
		Meta("openapi:extension:x-speakeasy-name-override", "creditUsage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetCreditUsage"}`)
	})

	Method("deleteChat", func() {
		Description("Soft-delete a chat by its ID")
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The ID of the chat to delete")
			Required("id")
		})

		HTTP(func() {
			DELETE("/rpc/chat.delete")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteChat")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})

	Method("setPinned", func() {
		Description("Pin or unpin a chat. Pinned chats surface in a dedicated section above recents on the chat page.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The ID of the chat to pin or unpin")
			Attribute("pinned", Boolean, "True to pin the chat, false to unpin it")
			Required("id", "pinned")
		})

		HTTP(func() {
			POST("/rpc/chat.setPinned")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "setChatPinned")
		Meta("openapi:extension:x-speakeasy-name-override", "setPinned")
	})

	Method("submitFeedback", func() {
		Description("Submit user feedback for a chat (success/failure)")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("id", String, "The ID of the chat")
			Attribute("feedback", String, "User feedback: success or failure", func() {
				Enum("success", "failure")
			})
			Required("id", "feedback")
		})

		Result(func() {
			Attribute("success", Boolean, "Whether the feedback was submitted successfully")
			Required("success")
		})

		HTTP(func() {
			POST("/rpc/chat.submitFeedback")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "submitFeedback")
		Meta("openapi:extension:x-speakeasy-name-override", "submitFeedback")
	})

	Method("listSources", func() {
		Description("List the distinct agent sources present in this project's chats, for populating the agent-type filter on the Agent Sessions page.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
		})

		Result(ListSourcesResult)

		HTTP(func() {
			GET("/rpc/chat.listSources")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listChatSources")
		Meta("openapi:extension:x-speakeasy-name-override", "listSources")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListChatSources", "type": "query"}`)
	})
})

var ListSourcesResult = Type("ListSourcesResult", func() {
	Attribute("sources", ArrayOf(String), "The distinct agent sources present in this project's chats (raw source strings such as 'claude-code', 'Codex', 'playground').")
	Required("sources")
})

var ListChatsResult = Type("ListChatsResult", func() {
	Attribute("chats", ArrayOf(ChatOverview), "The list of chats")
	Attribute("total", Int, "Total number of chats (before pagination)")
	Required("chats", "total")
})

var ChatOverview = Type("ChatOverview", func() {
	Attribute("id", String, "The ID of the chat")
	Attribute("title", String, "The title of the chat")
	Attribute("user_id", String, "The ID of the user who created the chat")
	Attribute("external_user_id", String, "The ID of the external user who created the chat")
	Attribute("num_messages", Int, "The number of messages in the chat")
	Attribute("source", String, "The source of the chat: Elements, Playground, ClaudeCode (inferred from messages)")
	Attribute("created_at", String, func() {
		Description("When the chat was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the chat was last updated.")
		Format(FormatDateTime)
	})
	Attribute("total_input_tokens", Int64, "Total input tokens used in this chat")
	Attribute("total_output_tokens", Int64, "Total output tokens used in this chat")
	Attribute("total_tokens", Int64, "Total tokens (input + output) used in this chat")
	Attribute("total_cost", Float64, "Total cost in USD for this chat")
	Attribute("last_message_timestamp", String, func() {
		Description("When the last message in the chat was created.")
		Format(FormatDateTime)
	})
	Attribute("risk_findings_count", Int, "Number of risk findings recorded against messages in this chat (project-scoped, found=true). Only populated by endpoints that join risk data; absent elsewhere.")
	Attribute("account_type", String, "Account type that produced the chat ('team', 'personal', or empty), resolved from the linked AI account.")

	Required("id", "title", "num_messages", "created_at", "updated_at", "last_message_timestamp")
})

var Chat = Type("Chat", func() {
	Extend(ChatOverview)
	Attribute("messages", ArrayOf(ChatMessage), "The list of messages in the chat for the returned generation, ordered oldest to newest by `seq`.")
	Attribute("generation", Int, "The generation that this response's messages belong to. A generation is an immutable snapshot of the transcript; a new one is opened on compaction or message edits, while normal turns append to the current one.")
	Attribute("max_generation", Int, "The highest generation number present for this chat. To load the full history, walk from `max_generation` down to 0, requesting each generation in turn.")
	Attribute("has_more_before", Boolean, "Whether older messages exist before the first message in this page (within the returned generation). Load them with a `before_seq` cursor.")
	Attribute("has_more_after", Boolean, "Whether newer messages exist after the last message in this page (within the returned generation). Load them with an `after_seq` cursor.")
	Attribute("risk_segments", ArrayOf(RiskSegment), "Present only when `risk_only` was requested: contiguous runs of returned messages, each spanning a risk finding and its surrounding context. Use each segment's cursors to expand it.")
	Attribute("match_segments", ArrayOf(RiskSegment), "Present only when `query` was requested: contiguous runs of returned messages, each spanning one or more query matches and their surrounding context. Use each segment's cursors to expand it.")
	Attribute("agent_usage", AgentUsage, "Agent-specific usage enrichment for the chat, when available.")
	Attribute("totals", ChatTotals, "Whole-generation trace-entry totals for the returned generation. Because messages are paginated, callers must use these (not the length of `messages`) to render filter-bar counts.")

	Required("messages", "generation", "max_generation", "has_more_before", "has_more_after")
})

var ChatTotals = Type("ChatTotals", func() {
	Description("Trace-entry counts across the entire returned generation, independent of pagination. Each message maps to exactly one entry: a message carrying tool calls counts as a tool call regardless of role, otherwise the role decides.")
	Attribute("total", Int64, "Total trace entries in the generation (sum of the four entry-type counts; the `of N entries` denominator).")
	Attribute("user_messages", Int64, "Number of user messages in the generation.")
	Attribute("assistant_messages", Int64, "Number of assistant messages (without tool calls) in the generation.")
	Attribute("tool_calls", Int64, "Number of messages carrying tool calls in the generation.")
	Attribute("tool_results", Int64, "Number of tool-result messages in the generation.")
	Attribute("risk_only", Int64, "Number of messages with an active (found, non-suppressed) risk finding in the generation.")

	Required("total", "user_messages", "assistant_messages", "tool_calls", "tool_results", "risk_only")
})

var RiskSegment = Type("RiskSegment", func() {
	Description("A contiguous run of messages in a windowed view (`risk_only` via `risk_segments`, or `query` via `match_segments`), covering one or more matches plus their surrounding context. Messages for a segment are the entries of `Chat.messages` whose `seq` falls within `[first_seq, last_seq]`.")
	Attribute("first_seq", Int64, "The `seq` of the first (oldest) message in this segment.")
	Attribute("last_seq", Int64, "The `seq` of the last (newest) message in this segment.")
	Attribute("has_more_before", Boolean, "Whether messages exist before this segment within the generation. Expand with a `before_seq` request using `first_seq`.")
	Attribute("has_more_after", Boolean, "Whether messages exist after this segment within the generation. Expand with an `after_seq` request using `last_seq`.")

	Required("first_seq", "last_seq", "has_more_before", "has_more_after")
})

var ChatMessage = Type("ChatMessage", func() {
	Attribute("id", String, "The ID of the message")
	Attribute("seq", Int64, "Monotonic sequence number of the message. Strictly increasing within a chat; use it as the keyset cursor for `before_seq`/`after_seq` pagination. Not contiguous (the sequence is shared across chats), so do not infer gaps from arithmetic differences.")
	Attribute("is_risk", Boolean, "Present only in `risk_only` mode: true when this message has an active risk finding, false for the surrounding-context messages padded around it.")
	Attribute("role", String, "The role of the message")
	Attribute("content", Any, "The content of the message — string for plain text, array for multimodal/tool-call content parts, null for assistant messages that only carry tool_calls", func() {
		Meta("struct:field:type", "json.RawMessage", "encoding/json")
	})
	Attribute("model", String, "The model that generated the message")
	Attribute("tool_call_id", String, "The tool call ID of the message")
	Attribute("tool_calls", String, "The tool calls in the message as a JSON blob")
	Attribute("finish_reason", String, "The finish reason of the message")
	Attribute("prompt_id", String, "The agent prompt/turn ID associated with this message, when available.")
	Attribute("user_id", String, "The ID of the user who created the message")
	Attribute("external_user_id", String, "The ID of the external user who created the message")
	Attribute("created_at", String, func() {
		Description("When the message was created.")
		Format(FormatDateTime)
	})
	Attribute("generation", Int, "Conversation generation — bumps on compaction or edit divergence")

	Required("id", "seq", "role", "model", "created_at", "generation")
})

var AgentUsage = Type("AgentUsage", func() {
	Attribute("type", String, "The agent usage payload discriminator.", func() {
		Enum("claude")
	})
	Attribute("claude", ClaudeAgentUsage, "Claude Code usage details.")

	Required("type")
})

var ClaudeAgentUsage = Type("ClaudeAgentUsage", func() {
	Attribute("turns", ArrayOf(ClaudeTurnUsage), "Per-prompt Claude usage turns ordered by start time.")
	Attribute("tools", ArrayOf(ClaudeToolUsage), "Per-tool Claude usage keyed by tool_use_id.")

	Required("turns", "tools")
})

var ClaudeTurnUsage = Type("ClaudeTurnUsage", func() {
	Attribute("prompt_id", String, "Claude prompt.id that correlates events for one user turn.")
	Attribute("start_time_unix_nano", String, "Earliest OTEL log timestamp in this turn, as Unix nanoseconds.")
	Attribute("end_time_unix_nano", String, "Latest OTEL log timestamp in this turn, as Unix nanoseconds.")
	Attribute("request_count", Int64, "Number of Claude API request events in this turn.")
	Attribute("input_tokens", Int64, "Input tokens used by this turn.")
	Attribute("output_tokens", Int64, "Output tokens used by this turn.")
	Attribute("cache_read_tokens", Int64, "Cache read tokens used by this turn.")
	Attribute("cache_creation_tokens", Int64, "Cache creation tokens used by this turn.")
	Attribute("total_tokens", Int64, "Total tokens used by this turn.")
	Attribute("cost_usd", Float64, "Total USD cost for this turn.")
	Attribute("cost_micros", Int64, "Total cost for this turn in micros of a USD.")
	Attribute("models", ArrayOf(String), "Distinct model names used by this turn.")
	Attribute("query_sources", ArrayOf(String), "Distinct Claude query sources used by this turn.")

	Required("prompt_id", "start_time_unix_nano", "end_time_unix_nano", "request_count", "input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens", "cost_usd", "cost_micros", "models", "query_sources")
})

var ClaudeToolUsage = Type("ClaudeToolUsage", func() {
	Attribute("tool_use_id", String, "Claude tool_use_id that correlates the tool call and result.")
	Attribute("prompt_id", String, "Claude prompt.id for the turn that used this tool.")
	Attribute("tool_name", String, "Tool name reported by Claude Code.")
	Attribute("input_size_bytes", Int64, "Serialized tool input size in bytes.")
	Attribute("result_size_bytes", Int64, "Serialized tool result size in bytes.")

	Required("tool_use_id", "prompt_id", "tool_name", "input_size_bytes", "result_size_bytes")
})
