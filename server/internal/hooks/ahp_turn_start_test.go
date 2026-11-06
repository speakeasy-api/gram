package hooks

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func ahpTestTurnStart() map[string]any {
	e := ahpTestInbound()
	delete(e, "message")
	e["type"] = "turn.start"
	e["trigger"] = "user"
	e["turn"] = map[string]any{"id": "turn"}
	e["items"] = []any{ahpTestMessage("user", "incoming prompt")}
	return e
}

func TestAHPTurnStartPromptOwnership(t *testing.T) {
	t.Parallel()
	for _, trigger := range []string{"user", "external", "continuation", "hook"} {
		t.Run(trigger, func(t *testing.T) {
			t.Parallel()
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
					parts = []any{ahpTestPart("safe prompt"), ahpTestAttachmentPart("notes", "application/xml", ref)}
				}
				e["items"] = []any{map[string]any{"id": "message", "role": "user", "parts": parts}}
				if mode == "observe" {
					w := ahpRequest(t, h, "hooks/observe", e, nil, ahpAuthHeaders)
					require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
				} else {
					effects := []string{"deny"}
					if mode == "no-deny" {
						effects = []string{}
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

func TestAHPTurnStartAttachmentCategoryParity(t *testing.T) {
	t.Parallel()
	for _, category := range []string{"", "document"} {
		for _, boundary := range []string{"arrival", "admission"} {
			t.Run(category+"/"+boundary, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestHooksService(t)
				ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
				scanner := &ahpContentMatchingScanner{}
				ti.service.riskScanner = scanner
				h, err := ti.service.ahpHandler()
				require.NoError(t, err)
				ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte("RESTRICTED_TEST_CONTENT"))
				part := ahpTestAttachmentPart("notes", "application/xml", ref)
				if category != "" {
					part["category"] = category
				}
				items := []any{map[string]any{"id": "message", "role": "user", "parts": []any{part}}}
				e := ahpTestTurnStart()
				e["items"] = items
				if boundary == "arrival" {
					e = ahpTestInbound()
					e["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": items}
				}
				w := ahpRequest(t, h, "hooks/intercept", e, []string{"deny"}, ahpAuthHeaders)
				require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
				require.True(t, ahpResponseHasEffects(w.Body.Bytes()))
				require.Len(t, scanner.requests, 1)
				require.Equal(t, "RESTRICTED_TEST_CONTENT", scanner.requests[0].Text)
			})
		}
	}
}

func TestAHPTurnStartAttachmentUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	auth := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte("attachment evidence"))
	require.NoError(t, ti.service.cache.Delete(ctx, ahpUploadCacheKey(*auth.ProjectID, strings.TrimPrefix(ref, ahpUploadRefPrefix))))
	e := ahpTestTurnStart()
	session := uuid.NewString()
	e["session"] = map[string]any{"id": session}
	e["items"] = []any{map[string]any{"id": "message", "role": "user", "parts": []any{
		ahpTestPart("safe inline prompt"),
		ahpTestAttachmentPart("missing", "application/xml", ref),
	}}}
	b, err := json.Marshal(e)
	require.NoError(t, err)
	var typed ahp.TurnStartEvent
	require.NoError(t, json.Unmarshal(b, &typed))
	_, err = ti.service.ahpPromptAttachments(ctx, ahpTurnStartPrompt(&typed))
	require.ErrorIs(t, err, errAHPAttachmentUnavailable)
	w := ahpRequest(t, h, "hooks/intercept", e, []string{"deny"}, ahpAuthHeaders)
	var envelope struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, -32004, envelope.Error.Code)
	require.Equal(t, "Backend internal error", envelope.Error.Message)
	require.Empty(t, envelope.Result)
	w = ahpRequest(t, h, "hooks/observe", e, nil, ahpAuthHeaders)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Empty(t, w.Body.String(), "notifications do not receive RPC errors")
	require.Zero(t, scanner.scans)
	rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: sessionIDToUUID(session), ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.Empty(t, rows, "neither boundary may persist a partial inline prompt")
}

// The same admitted prompt can appear at both boundaries. Message identity
// correlates persistence, but each intercept still evaluates its own evidence.
func TestAHPTurnStartAndInboundCorrelation(t *testing.T) {
	t.Parallel()
	for _, harness := range []string{"opencode", "kit", "example-harness"} {
		for _, order := range []string{"arrival-first", "admission-first"} {
			t.Run(harness+"/"+order, func(t *testing.T) {
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
					arrival := ahpTestInbound()
					arrivalMessage := ahpTestMessage("user", "incoming prompt")
					arrivalMessage["id"] = turn + "-message"
					arrival["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{arrivalMessage}}
					admission := ahpTestTurnStart()
					admission["items"] = []any{arrivalMessage}
					admission["turn"] = map[string]any{"id": turn + "-message"}
					events := []map[string]any{arrival, admission}
					if order == "admission-first" {
						events[0], events[1] = events[1], events[0]
					}
					midturn := ahpTestInbound()
					midturn["turn"] = map[string]any{"id": turn + "-message"}
					midturnMessage := ahpTestMessage("user", "incoming prompt")
					midturnMessage["id"] = turn + "-midturn-message"
					midturn["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{midturnMessage}}
					events = append(events, midturn)
					for _, e := range events {
						e["id"] = uuid.NewString()
						e["session"] = map[string]any{"id": session}
						e["harness"] = map[string]any{"name": harness, "version": "1.0.0"}
						w := ahpRequest(t, h, "hooks/intercept", e, []string{"deny"}, ahpAuthHeaders)
						require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
					}
				}
				require.Equal(t, 6, scanner.scans)
				rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: sessionIDToUUID(session), ProjectID: *auth.ProjectID})
				require.NoError(t, err)
				require.Len(t, rows, 4, "distinct messages in one turn and repeated text in distinct turns remain distinct")
			})
		}
	}
}

// OpenCode's canonical message ID is its native prompt/turn identity. Prove
// compatibility with the provider-turn key used by LiteLLM, including the
// correlated writer's promotion of an existing proxy row to the native source.
func TestAHPOpenCodePromptProxyCorrelation(t *testing.T) {
	t.Parallel()
	for _, proxyOrder := range []string{"proxy-first", "native-first"} {
		for _, arrivalOrder := range []string{"arrival-first", "admission-first"} {
			t.Run(proxyOrder+"/"+arrivalOrder, func(t *testing.T) {
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
				messageID := "msg_" + uuid.NewString()
				proxy := canonicalIngestPayload("litellm", "prompt.submitted", session)
				proxy.IdempotencyKey = new(uuid.NewString())
				proxy.Session.TurnID = new(agentTurnPrefix + "opencode:" + messageID)
				proxy.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: new("incoming prompt")}}
				options := defaultAuthenticatedIngestOptions()
				options.ObserveOnly = true
				if proxyOrder == "proxy-first" {
					_, err = ti.service.IngestAuthenticatedWithOptions(ctx, auth, proxy, options)
					require.NoError(t, err)
				}
				item := ahpTestMessage("user", "incoming prompt")
				item["id"] = messageID
				arrival := ahpTestInbound()
				arrival["message"] = map[string]any{"sender": "user", "channel": "prompt", "messages": []any{item}}
				admission := ahpTestTurnStart()
				admission["turn"] = map[string]any{"id": messageID}
				admission["items"] = []any{item}
				events := []map[string]any{arrival, admission}
				if arrivalOrder == "admission-first" {
					events[0], events[1] = events[1], events[0]
				}
				for _, e := range events {
					e["session"] = map[string]any{"id": session}
					e["harness"] = map[string]any{"name": "opencode", "version": "0.1.0"}
					w := ahpRequest(t, h, "hooks/intercept", e, []string{"deny"}, ahpAuthHeaders)
					require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
				}
				if proxyOrder == "native-first" {
					_, err = ti.service.IngestAuthenticatedWithOptions(ctx, auth, proxy, options)
					require.NoError(t, err)
				}
				require.Equal(t, 2, scanner.scans)
				rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: sessionIDToUUID(session), ProjectID: *auth.ProjectID})
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, "opencode", rows[0].Source.String)
				require.Equal(t, "incoming prompt", rows[0].Content)
			})
		}
	}
}
