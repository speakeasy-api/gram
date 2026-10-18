package hooks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

func ahpTestPart(text string) map[string]any {
	return map[string]any{"id": "part", "kind": "text", "mediaType": "text/plain", "selection": "body", "text": text}
}

func ahpTestMessage(role, text string) map[string]any {
	return map[string]any{"id": "message", "role": role, "parts": []any{ahpTestPart(text)}}
}

func ahpTestInbound() map[string]any {
	e := ahpToolEvent("user.message.inbound")
	delete(e, "tool")
	delete(e, "call")
	delete(e, "path")
	e["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{
		ahpTestMessage("user", "incoming prompt"),
	}}
	return e
}

func ahpTestResponse() map[string]any {
	e := ahpToolEvent("model.response.after")
	delete(e, "tool")
	delete(e, "call")
	delete(e, "path")
	e["model"] = map[string]any{"id": "example", "provider": "example"}
	e["attempt"] = map[string]any{"id": "attempt", "number": 1}
	e["execution"] = map[string]any{"status": "executed"}
	e["finishReason"] = "stop"
	e["items"] = []any{ahpTestMessage("assistant", "same text")}
	e["usage"] = map[string]any{"kind": "amount", "scope": "attempt", "completeness": "complete", "provenance": "provider", "inputTokens": 12, "outputTokens": 3, "cost": map[string]any{"amount": 0.01, "currency": "USD", "basis": "reported"}}
	return e
}

func TestAHPInboundPromptOwnership(t *testing.T) {
	t.Parallel()
	raw := ahpTestInbound()
	var e ahp.UserMessageInboundEvent
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &e))
	p := ahpUserMessageInboundPayload(&e, nil)
	require.Equal(t, "prompt.submitted", p.Event.Type)
	require.Equal(t, "incoming prompt", *p.Data.Prompt.Text)
	// The wire schema currently allows only user messages. Keep the adapter
	// filter explicit as defense in depth if a future SDK broadens that role.
	for _, role := range []string{"system", "assistant"} {
		e.Message.Messages[0].Role = ahp.UserMessageInboundEventMessageMessagesItemRole(role)
		require.Nil(t, ahpUserMessageInboundPayload(&e, nil))
	}
}

func TestAHPInboundPolicyAndLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	for _, method := range []string{"hooks/intercept", "hooks/observe"} {
		for _, deny := range []bool{true, false} {
			scanner := &countingBlockScanner{stubResultScanner: stubResultScanner{result: &risk.ScanResult{Action: "block", PolicyName: "Example policy"}}}
			ti.service.riskScanner = scanner
			effects := []string{}
			if deny {
				effects = append(effects, "deny")
			}
			w := ahpRequest(t, h, method, ahpTestInbound(), effects, ahpAuthHeaders)
			if method == "hooks/observe" {
				require.Equal(t, http.StatusNoContent, w.Code)
			} else {
				require.Equal(t, http.StatusOK, w.Code)
				require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
				require.Equal(t, deny, ahpResponseHasEffects(w.Body.Bytes()))
			}
			require.Equal(t, method == "hooks/intercept" && deny, scanner.scans > 0)
		}
	}
	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner
	e := ahpTestInbound()
	delete(e, "message")
	e["type"] = "turn.start"
	e["trigger"] = "user"
	e["turn"] = map[string]any{"id": "turn"}
	e["items"] = []any{ahpTestMessage("user", "duplicate prompt")}
	w := ahpRequest(t, h, "hooks/intercept", e, []string{"deny"}, ahpAuthHeaders)
	require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
	require.False(t, ahpResponseHasEffects(w.Body.Bytes()))
	require.Zero(t, scanner.scans)
}

func ahpResponseHasEffects(b []byte) bool {
	var response struct {
		Result struct {
			Effects []map[string]any `json:"effects"`
		} `json:"result"`
	}
	if json.Unmarshal(b, &response) != nil {
		return false
	}
	return len(response.Result.Effects) > 0
}

func TestAHPResponseFanoutIdentitiesAndCategories(t *testing.T) {
	t.Parallel()
	raw := ahpTestResponse()
	parts := []any{ahpTestPart("legacy")}
	for _, category := range []string{"text", "tool-call", "reasoning", "tool-result", "unknown", ""} {
		p := ahpTestPart(category)
		p["category"] = category
		parts = append(parts, p)
	}
	message := ahpTestMessage("assistant", "")
	message["parts"] = parts
	raw["items"] = []any{ahpTestMessage("system", "PRIVATE"), message, message, ahpTestMessage("user", "PRIVATE")}
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	var e ahp.ModelResponseAfterEvent
	require.NoError(t, json.Unmarshal(b, &e))
	payloads := ahpModelResponsePayloads(&e)
	require.Len(t, payloads, 3)
	require.Equal(t, "usage.reported", payloads[0].Event.Type)
	require.Equal(t, 12, *payloads[0].Data.Usage.InputTokens)
	require.Equal(t, payloads, ahpModelResponsePayloads(&e))
	require.NotEqual(t, *payloads[1].IdempotencyKey, *payloads[2].IdempotencyKey)
	for _, p := range payloads[1:] {
		require.Equal(t, "legacy\ntext", *p.Data.Message.Text)
		require.Equal(t, "assistant", *p.Data.Message.Role)
		require.Nil(t, p.Data.Usage)
		require.Nil(t, p.Data.ToolCall)
		require.NotEqual(t, *payloads[0].IdempotencyKey, *p.IdempotencyKey)
	}
	e.ID = "different-response"
	require.NotEqual(t, *payloads[1].IdempotencyKey, *ahpModelResponsePayloads(&e)[1].IdempotencyKey)
}

func TestAHPAuthoritativeTranscriptReplay(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ch := enableHookTelemetryLogger(t, ctx, ti)
	auth := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	session := uuid.NewString()
	start := time.Now().UTC().Add(-time.Minute)
	events := []map[string]any{ahpTestInbound()}
	turn := ahpTestInbound()
	delete(turn, "message")
	turn["type"] = "turn.start"
	turn["trigger"] = "user"
	turn["items"] = []any{ahpTestMessage("user", "incoming prompt")}
	events = append(events, turn)
	request := ahpBoundaryEvent("model.request.before")
	request["items"] = []any{ahpTestMessage("user", "incoming prompt"), ahpTestMessage("assistant", "same text")}
	events = append(events, request)
	commentary := ahpTestResponse()
	rich := ahpTestMessage("assistant", "")
	text := ahpTestPart("second commentary")
	text["category"] = "text"
	reasoning := ahpTestPart("PRIVATE_REASONING")
	reasoning["category"] = "reasoning"
	call := ahpTestPart("PRIVATE_TOOL_CALL")
	call["category"] = "tool-call"
	rich["parts"] = []any{text, reasoning, call}
	onlyCall := ahpTestMessage("assistant", "")
	onlyCall["parts"] = []any{call}
	commentary["items"] = []any{ahpTestMessage("assistant", "commentary"), rich, onlyCall, ahpTestMessage("system", "PRIVATE_SYSTEM"), ahpTestMessage("user", "PRIVATE_USER")}
	events = append(events, commentary)
	for _, kind := range []string{"tool.before", "tool.after"} {
		e := ahpToolEvent(kind)
		if kind == "tool.after" {
			e["execution"] = map[string]any{"status": "executed"}
			e["outcome"] = "ok"
			output := ahpTestPart("tool result")
			output["category"] = "tool-result"
			message := ahpTestMessage("tool", "")
			message["parts"] = []any{output}
			e["items"] = []any{message}
		}
		events = append(events, e)
	}
	events = append(events, ahpTestResponse(), ahpTestResponse())
	outbound := ahpTestInbound()
	outbound["type"] = "user.message.outbound"
	outbound["message"] = map[string]any{"channel": "cli", "messages": []any{ahpTestMessage("assistant", "same text")}}
	events = append(events, outbound)
	end := ahpTestInbound()
	delete(end, "message")
	end["type"] = "turn.end"
	end["outcome"] = "completed"
	end["continuationCount"] = 0
	end["items"] = []any{ahpTestMessage("assistant", "same text")}
	events = append(events, end)
	for i, e := range events {
		e["id"] = fmt.Sprintf("event-%d", i)
		e["session"] = map[string]any{"id": session}
		e["turn"] = map[string]any{"id": "turn"}
		e["time"] = start.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		frame, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "hooks/observe", "params": map[string]any{"protocolVersion": "draft", "event": e}})
		require.NoError(t, err)
		parsed := ahp.ParseObserveNotification(frame)
		require.True(t, parsed.OK, "%s: %v", e["type"], parsed.Diagnostics)
	}
	for delivery := range 2 {
		for _, e := range events {
			if delivery == 0 && (e["type"] == "model.response.after" || e["type"] == "tool.after") {
				ahpBoundaryRequest(t, h, e, map[string]any{"effects": []string{}})
				continue
			}
			w := ahpRequest(t, h, "hooks/observe", e, nil, ahpAuthHeaders)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
		}
	}
	require.Zero(t, scanner.scans)
	rows, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: sessionIDToUUID(session), ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.Len(t, rows, 7)
	want := []string{"incoming prompt", "commentary", "second commentary", "", `"tool result"`, "same text", "same text"}
	for i, row := range rows {
		require.Equal(t, want[i], row.Content, "row %d", i)
		if i != 3 {
			require.Empty(t, row.ToolCalls)
		}
	}
	require.Equal(t, "user", rows[0].Role)
	require.Equal(t, "tool", rows[4].Role)
	require.Contains(t, string(rows[3].ToolCalls), "read_file")
	require.NotEmpty(t, rows[4].ToolCallID.String)
	// Six original events, four assistant envelopes and three boundary audits.
	// The outbound and turn-end events and the replay add no transcript or usage.
	var logs []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		logs, err = ch.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{GramProjectID: auth.ProjectID.String(), TimeStart: start.Add(-time.Minute).UnixNano(), TimeEnd: time.Now().Add(time.Minute).UnixNano(), SortOrder: "asc", Limit: 100})
		return err == nil && len(logs) == 13
	}, 5*time.Second, 50*time.Millisecond)
	// Keep observing after the expected rows arrive so delayed replay writes
	// cannot hide behind the first successful poll.
	require.Never(t, func() bool {
		logs, err = ch.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{GramProjectID: auth.ProjectID.String(), TimeStart: start.Add(-time.Minute).UnixNano(), TimeEnd: time.Now().Add(time.Minute).UnixNano(), SortOrder: "asc", Limit: 100})
		return err != nil || len(logs) != 13
	}, time.Second, 50*time.Millisecond)
	var input, output, cost float64
	usageRows := 0
	for _, log := range logs {
		var stored struct {
			GenAI struct {
				Usage *struct {
					Input  float64 `json:"input_tokens"`
					Output float64 `json:"output_tokens"`
					Cost   float64 `json:"cost"`
				} `json:"usage"`
			} `json:"gen_ai"`
		}
		require.NoError(t, json.Unmarshal([]byte(log.Attributes), &stored))
		if u := stored.GenAI.Usage; u != nil {
			usageRows++
			input += u.Input
			output += u.Output
			cost += u.Cost
		}
	}
	require.Equal(t, 3, usageRows)
	require.InDelta(t, 36, input, 0.000001)
	require.InDelta(t, 9, output, 0.000001)
	require.InDelta(t, 0.03, cost, 0.000001)
}

func TestAHPResponseUsageBoundaries(t *testing.T) {
	t.Parallel()
	raw := ahpTestResponse()
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	var e ahp.ModelResponseAfterEvent
	require.NoError(t, json.Unmarshal(b, &e))
	e.Harness.Name = "kit"
	original := ahpModelResponsePayloads(&e)[0]
	require.Equal(t, "kit", original.Source.Adapter)
	require.Equal(t, "model.response.after", *original.Source.RawEventName)
	require.Nil(t, original.Data.Message)
	require.InDelta(t, 0.01, *original.Data.Usage.Cost, 0.000001)
	e.Usage.Value.Cost.Value.Currency = "EUR"
	require.Nil(t, ahpModelResponsePayloads(&e)[0].Data.Usage.Cost)
	e.Usage.Present = false
	require.Nil(t, ahpModelResponsePayloads(&e)[0].Data.Usage)
	for _, value := range []string{"-1", "1.5", "9223372036854775808"} {
		require.Nil(t, ahpTokenCount(ahp.Some(json.Number(value))))
	}
	require.Equal(t, 0, *ahpTokenCount(ahp.Some(json.Number("0"))))
}
