package chat_test

import (
	"context"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func newCaptureStrategy(t *testing.T, conn *pgxpool.Pool) *chat.ChatMessageCaptureStrategy {
	t.Helper()
	writer, writerShutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = writerShutdown(t.Context()) })
	return chat.NewChatMessageCaptureStrategy(
		testenv.NewLogger(t),
		conn,
		writer,
	)
}

func makeRequest(chatID, projectID uuid.UUID, orgID string, msgs ...or.ChatMessages) openrouter.CompletionRequest {
	return openrouter.CompletionRequest{
		OrgID:       orgID,
		ProjectID:   projectID.String(),
		ChatID:      chatID,
		Messages:    msgs,
		UsageSource: billing.ModelUsageSourcePlayground,
		Model:       "test-model",
	}
}

func makeResponse(content string) openrouter.CompletionResponse {
	return openrouter.CompletionResponse{
		Content:   content,
		Model:     "test-model",
		MessageID: "msg-" + content,
	}
}

func makeAssistantToolOnly(toolCalls ...or.ChatToolCall) or.ChatMessages {
	empty := or.CreateChatAssistantMessageContentStr("")
	return or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&empty),
		Name:             nil,
		ToolCalls:        toolCalls,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
}

// runTurn threads a single request through StartOrResumeChat + CaptureMessage
// the same way the unified client does — so tests exercise the session handoff.
func runTurn(t *testing.T, ctx context.Context, s *chat.ChatMessageCaptureStrategy, req openrouter.CompletionRequest, resp openrouter.CompletionResponse) {
	t.Helper()
	sess, err := s.StartOrResumeChat(ctx, req)
	require.NoError(t, err)
	require.NoError(t, s.CaptureMessage(ctx, sess, req, resp))
}

func listAllMessages(t *testing.T, ctx context.Context, conn *pgxpool.Pool, chatID, projectID uuid.UUID) []repo.ChatMessage {
	t.Helper()
	rows, err := repo.New(conn).ListChatMessages(ctx, repo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	return rows
}

// Clean append: reload with full history + new message should insert only the
// new message and stay on generation 0.
func TestMatcher_CleanAppend(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("Hello")),
		makeResponse("Hi there"),
	)

	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID,
			openrouter.CreateMessageUser("Hello"),
			openrouter.CreateMessageAssistant("Hi there"),
			openrouter.CreateMessageUser("How are you?"),
		),
		makeResponse("Doing well"),
	)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 4)
	for i, r := range rows {
		require.Equal(t, int32(0), r.Generation, "row %d on generation 0", i)
	}
	require.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(rows))
}

// Compaction: round 2 sends fewer messages than round 1 stored. Matcher should
// bump generation and start a fresh chain while keeping the old rows.
func TestMatcher_CompactionBumpsGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	seed := []or.ChatMessages{openrouter.CreateMessageUser("one")}
	runTurn(t, ctx, s, makeRequest(chatID, projectID, orgID, seed...), makeResponse("r1"))

	seed = append(seed,
		openrouter.CreateMessageAssistant("r1"),
		openrouter.CreateMessageUser("two"),
	)
	runTurn(t, ctx, s, makeRequest(chatID, projectID, orgID, seed...), makeResponse("r2"))

	beforeCount := len(listAllMessages(t, ctx, conn, chatID, projectID))
	require.Equal(t, 4, beforeCount)

	// Client compacts: sends a summary + new user message instead of full history.
	compacted := []or.ChatMessages{
		openrouter.CreateMessageSystem("Summary: user said hi, assistant said hi back."),
		openrouter.CreateMessageUser("continue"),
	}
	runTurn(t, ctx, s, makeRequest(chatID, projectID, orgID, compacted...), makeResponse("ok"))

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 7, "original 4 + compacted 3 (system, user, assistant)")

	gens := map[int32]int{}
	for _, r := range rows {
		gens[r.Generation]++
	}
	require.Equal(t, 4, gens[0], "pre-compaction rows stay at gen 0")
	require.Equal(t, 3, gens[1], "compacted chain starts at gen 1")
}

// Edit: same number of messages but content at index 0 differs. Treated as a
// new generation.
func TestMatcher_EditBumpsGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	round1 := []or.ChatMessages{openrouter.CreateMessageUser("original question")}
	runTurn(t, ctx, s, makeRequest(chatID, projectID, orgID, round1...), makeResponse("answer"))

	// Client edits the first user message and retries.
	round2 := []or.ChatMessages{openrouter.CreateMessageUser("edited question")}
	runTurn(t, ctx, s, makeRequest(chatID, projectID, orgID, round2...), makeResponse("different answer"))

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 4)
	require.Equal(t, int32(0), rows[0].Generation)
	require.Equal(t, int32(0), rows[1].Generation)
	require.Equal(t, int32(1), rows[2].Generation)
	require.Equal(t, int32(1), rows[3].Generation)
}

func TestMatcher_ToolCallsMatchAcrossIndexAndOrderVariations(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	callA := openrouter.ToolCall{
		Index: 0,
		ID:    "call_a",
		Type:  "function",
		Function: openrouter.ToolCallFunction{
			Name:      "first_tool",
			Arguments: `{"value": "a", "other": 1}`,
		},
	}
	callB := openrouter.ToolCall{
		Index: 1,
		ID:    "call_b",
		Type:  "function",
		Function: openrouter.ToolCallFunction{
			Name:      "second_tool",
			Arguments: `{"value":"b"}`,
		},
	}

	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("use tools")),
		openrouter.CompletionResponse{
			Content:   "",
			Model:     "test-model",
			MessageID: "msg-tools",
			// Streaming capture used to range over map[int]ToolCall, so the
			// stored JSON order can differ from replayed request order.
			ToolCalls: []openrouter.ToolCall{callB, callA},
		},
	)

	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID,
			openrouter.CreateMessageUser("use tools"),
			makeAssistantToolOnly(
				or.ChatToolCall{
					ID:   callA.ID,
					Type: or.ChatToolCallTypeFunction,
					Function: or.ChatToolCallFunction{
						Name:      callA.Function.Name,
						Arguments: `{"other":1,"value":"a"}`,
					},
				},
				or.ChatToolCall{
					ID:   callB.ID,
					Type: or.ChatToolCallTypeFunction,
					Function: or.ChatToolCallFunction{
						Name:      callB.Function.Name,
						Arguments: callB.Function.Arguments,
					},
				},
			),
			openrouter.CreateMessageUser("next"),
		),
		makeResponse("done"),
	)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 4)
	require.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(rows))
	for i, r := range rows {
		require.Equal(t, int32(0), r.Generation, "row %d stays on gen 0", i)
	}
}

// Tool-call identity is the model-issued ID, not the args. If the same call
// id replays with mutated args (different runners or middleware reformatting
// the JSON, lossy round-trips, etc.) the slot must still match — anything
// else trips a generation bump on every turn that issued a tool call.
func TestMatcher_ToolCallArgumentMutationDoesNotBumpGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("weather")),
		openrouter.CompletionResponse{
			Content:   "",
			Model:     "test-model",
			MessageID: "msg-tools",
			ToolCalls: []openrouter.ToolCall{{
				Index: 0,
				ID:    "call_weather",
				Type:  "function",
				Function: openrouter.ToolCallFunction{
					Name:      "get_weather",
					Arguments: `{"city":"SF"}`,
				},
			}},
		},
	)

	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID,
			openrouter.CreateMessageUser("weather"),
			makeAssistantToolOnly(or.ChatToolCall{
				ID:   "call_weather",
				Type: or.ChatToolCallTypeFunction,
				Function: or.ChatToolCallFunction{
					Name:      "get_weather_v2",
					Arguments: `{"city":"NYC","unit":"f"}`,
				},
			}),
			openrouter.CreateMessageUser("next"),
		),
		makeResponse("done"),
	)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 4)
	for i, r := range rows {
		require.Equal(t, int32(0), r.Generation, "row %d stays on gen 0 — same call id", i)
	}
}

// A tool result whose body re-serializes with different whitespace, key
// ordering, or wrapper shape on replay (e.g. UI hydrates → parses JSON →
// re-stringifies through the AI SDK) must still match the stored row by
// tool_call_id alone. Including the body in the slot would force a generation
// bump on every turn that follows a tool call.
func TestMatcher_ToolResultBodyDriftDoesNotBumpGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("weather")),
		openrouter.CompletionResponse{
			Content:   "",
			Model:     "test-model",
			MessageID: "msg-tools",
			ToolCalls: []openrouter.ToolCall{{
				Index: 0,
				ID:    "call_weather",
				Type:  "function",
				Function: openrouter.ToolCallFunction{
					Name:      "get_weather",
					Arguments: `{"city":"SF"}`,
				},
			}},
		},
	)

	storedToolResult := `{"temp":72,"unit":"f"}`
	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID,
			openrouter.CreateMessageUser("weather"),
			makeAssistantToolOnly(or.ChatToolCall{
				ID:   "call_weather",
				Type: or.ChatToolCallTypeFunction,
				Function: or.ChatToolCallFunction{
					Name:      "get_weather",
					Arguments: `{"city":"SF"}`,
				},
			}),
			or.CreateChatMessagesTool(or.ChatToolMessage{
				Role:       or.ChatToolMessageRoleTool,
				Content:    or.CreateChatToolMessageContentStr(storedToolResult),
				ToolCallID: "call_weather",
			}),
			openrouter.CreateMessageUser("next"),
		),
		makeResponse("ok"),
	)

	driftedToolResult := `{"unit": "f", "temp": 72}`
	runTurn(t, ctx, s,
		makeRequest(chatID, projectID, orgID,
			openrouter.CreateMessageUser("weather"),
			makeAssistantToolOnly(or.ChatToolCall{
				ID:   "call_weather",
				Type: or.ChatToolCallTypeFunction,
				Function: or.ChatToolCallFunction{
					Name:      "get_weather",
					Arguments: `{"city":"SF"}`,
				},
			}),
			or.CreateChatMessagesTool(or.ChatToolMessage{
				Role:       or.ChatToolMessageRoleTool,
				Content:    or.CreateChatToolMessageContentStr(driftedToolResult),
				ToolCallID: "call_weather",
			}),
			openrouter.CreateMessageUser("next"),
			openrouter.CreateMessageAssistant("ok"),
			openrouter.CreateMessageUser("again"),
		),
		makeResponse("done"),
	)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	for i, r := range rows {
		require.Equal(t, int32(0), r.Generation, "row %d stays on gen 0 — tool result body drift is not part of identity", i)
	}
}

func roles(rows []repo.ChatMessage) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Role
	}
	return out
}

// An assistant response that carries both narrative text and tool_calls is a
// single model response and should land as a single stored row. OpenRouter
// replay normalization decides whether provider-specific wire payloads keep or
// drop the narrative text.
func TestCaptureMessage_StoresAssistantResponseWithBothTextAndToolCallsInSingleRow(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	req := makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("Check weather"))
	resp := openrouter.CompletionResponse{
		Content:   "I'll check the weather.",
		Model:     "test-model",
		MessageID: "msg-mixed",
		ToolCalls: []openrouter.ToolCall{{
			Index: 0,
			ID:    "tool_abc",
			Type:  "function",
			Function: openrouter.ToolCallFunction{
				Name:      "get_weather",
				Arguments: `{"city":"SF"}`,
			},
		}},
		Usage: openrouter.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	runTurn(t, ctx, s, req, resp)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 2)
	require.Equal(t, []string{"user", "assistant"}, roles(rows))

	assistant := rows[1]
	require.Equal(t, "I'll check the weather.", assistant.Content)
	require.NotEmpty(t, assistant.ToolCalls)
	require.Equal(t, int64(10), assistant.PromptTokens)
	require.Equal(t, int64(5), assistant.CompletionTokens)
	require.Equal(t, int64(15), assistant.TotalTokens)
}

// Whitespace-only assistant response content is treated as no text so storage
// does not preserve invisible narrative around tool calls.
func TestCaptureMessage_WhitespaceOnlyContentWithToolCallsStoresSingleRow(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTestChatContext(t)
	s := newCaptureStrategy(t, conn)
	chatID := uuid.New()

	req := makeRequest(chatID, projectID, orgID, openrouter.CreateMessageUser("Check weather"))
	resp := openrouter.CompletionResponse{
		Content:   "   \n\t",
		Model:     "test-model",
		MessageID: "msg-ws",
		ToolCalls: []openrouter.ToolCall{{
			Index: 0,
			ID:    "tool_abc",
			Type:  "function",
			Function: openrouter.ToolCallFunction{
				Name:      "get_weather",
				Arguments: `{"city":"SF"}`,
			},
		}},
		Usage: openrouter.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	runTurn(t, ctx, s, req, resp)

	rows := listAllMessages(t, ctx, conn, chatID, projectID)
	require.Len(t, rows, 2, "whitespace-only content collapses into one assistant row")
	require.Equal(t, []string{"user", "assistant"}, roles(rows))

	tools := rows[1]
	require.Empty(t, tools.Content, "stored Content is empty, not the whitespace")
	require.NotEmpty(t, tools.ToolCalls)
}
