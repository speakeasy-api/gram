package hooks

import (
	"encoding/json"
	"net/http"
	"testing"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func ahpTestTurnStart() map[string]any {
	e := ahpTestInbound()
	delete(e, "message")
	e["type"] = "turn.start"
	e["trigger"] = "user"
	e["items"] = []any{ahpTestMessage("user", "incoming prompt")}
	return e
}

func TestAHPTurnStartPromptOwnership(t *testing.T) {
	t.Parallel()
	for _, trigger := range []string{"user", "external", "continuation", "hook"} {
		t.Run(trigger, func(t *testing.T) {
			raw := ahpTestTurnStart()
			raw["trigger"] = trigger
			user := ahpTestMessage("user", "")
			reasoning := ahpTestPart("PRIVATE_REASONING")
			reasoning["category"] = "reasoning"
			result := ahpTestPart("PRIVATE_RESULT")
			result["category"] = "tool-result"
			user["parts"] = []any{reasoning, result, ahpTestPart("actual prompt")}
			raw["items"] = []any{ahpTestMessage("assistant", "PRIVATE_ASSISTANT"), ahpTestMessage("tool", "PRIVATE_TOOL"), user}
			b, err := json.Marshal(raw)
			require.NoError(t, err)
			var e ahp.TurnStartEvent
			require.NoError(t, json.Unmarshal(b, &e))
			prompt := ahpTurnStartPrompt(&e)
			if trigger == "continuation" || trigger == "hook" {
				require.Nil(t, prompt)
				return
			}
			p := ahpUserMessageInboundPayload(prompt, nil)
			require.Equal(t, "prompt.submitted", p.Event.Type)
			require.Equal(t, "turn.start", *p.Source.RawEventName)
			require.Equal(t, "actual prompt", *p.Data.Prompt.Text)
			e.Items = nil
			require.Nil(t, ahpUserMessageInboundPayload(ahpTurnStartPrompt(&e), nil))
		})
	}
}

func TestAHPTurnStartPromptPolicy(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"intercept", "observe", "no-deny"} {
		for _, attachment := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/text", true: "/attachment"}[attachment], func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestHooksService(t)
				ti.service.productFeatures = alwaysEnabledFeatures{}
				auth := hookAuthContext(t, ctx)
				ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
				scanner := &ahpContentMatchingScanner{}
				ti.service.riskScanner = scanner
				h, err := ti.service.ahpHandler()
				require.NoError(t, err)
				e := ahpTestTurnStart()
				session := uuid.NewString()
				e["session"] = map[string]any{"id": session}
				parts := []any{ahpTestPart("RESTRICTED_TEST_CONTENT")}
				if attachment {
					ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte("RESTRICTED_TEST_CONTENT"))
					parts = []any{ahpTestPart("safe prompt"), ahpTestAttachmentPart("notes", "text/plain", ref)}
				}
				e["items"] = []any{map[string]any{"id": "message", "role": "user", "parts": parts}}
				if mode == "observe" {
					w := ahpRequest(t, h, "hooks/observe", e, nil, ahpAuthHeaders)
					require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
				} else {
					effects := []string{"deny"}
					if mode == "no-deny" {
						effects = nil
					}
					w := ahpRequest(t, h, "hooks/intercept", e, effects, ahpAuthHeaders)
					require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
					require.Equal(t, mode == "intercept", ahpResponseHasEffects(w.Body.Bytes()))
				}
				require.Equal(t, mode == "intercept", len(scanner.requests) > 0)
				if mode != "intercept" {
					rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: sessionIDToUUID(session), ProjectID: *auth.ProjectID})
					require.NoError(t, err)
					require.Len(t, rows, 1)
					require.Equal(t, "user", rows[0].Role)
					require.Equal(t, map[bool]string{false: "RESTRICTED_TEST_CONTENT", true: "safe prompt"}[attachment], rows[0].Content)
					if attachment {
						parts, err := chatrepo.New(ti.conn).ListChatContentPartsByChatID(ctx, chatrepo.ListChatContentPartsByChatIDParams{
							ChatID: sessionIDToUUID(session), ProjectID: *auth.ProjectID, ParentChatMessageIds: []uuid.UUID{rows[0].ID},
						})
						require.NoError(t, err)
						require.Len(t, parts, 1)
					}
				}
			})
		}
	}
}

func TestAHPTurnStartAttachmentUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	e := ahpTestTurnStart()
	e["items"] = []any{map[string]any{"id": "message", "role": "user", "parts": []any{
		ahpTestPart("safe inline prompt"),
		ahpTestAttachmentPart("missing", "text/plain", ahpUploadRefPrefix+uuid.NewString()),
	}}}
	for _, mode := range []string{"hooks/intercept", "hooks/observe"} {
		w := ahpRequest(t, h, mode, e, []string{"deny"}, ahpAuthHeaders)
		require.Contains(t, w.Body.String(), `"error"`, "unavailable receipts must not produce a successful partial scan")
	}
}

// The same admitted prompt can appear at both boundaries. Native turn identity
// correlates persistence, but each intercept still evaluates its own evidence.
func TestAHPTurnStartAndInboundCorrelation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	auth := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	session := uuid.NewString()
	for _, turn := range []string{"first", "second"} {
		for _, e := range []map[string]any{ahpTestInbound(), ahpTestTurnStart()} {
			e["id"] = uuid.NewString()
			e["session"] = map[string]any{"id": session}
			e["turn"] = map[string]any{"id": turn}
			e["harness"] = map[string]any{"name": "opencode", "version": "0.1.0"}
			w := ahpRequest(t, h, "hooks/intercept", e, []string{"deny"}, ahpAuthHeaders)
			require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
		}
	}
	require.Equal(t, 4, scanner.scans)
	rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: sessionIDToUUID(session), ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.Len(t, rows, 2, "same text in distinct turns remains distinct")
}
