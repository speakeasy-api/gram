package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/sessionquarantine"
	"github.com/stretchr/testify/require"
)

type ahpEvidenceScanner struct {
	stubResultScanner
	requests []risk.RealtimeScanRequest
}

func (s *ahpEvidenceScanner) ScanForEnforcement(ctx context.Context, req risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	s.requests = append(s.requests, req)
	return s.stubResultScanner.ScanForEnforcement(ctx, req)
}

func ahpBoundaryRequest(t *testing.T, h http.Handler, event map[string]any, caps map[string]any, invalid ...bool) []map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": event["id"], "method": "hooks/intercept", "params": map[string]any{"protocolVersion": "draft", "event": event, "capabilities": caps}})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/ahp", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range ahpAuthHeaders {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	if len(invalid) > 0 && invalid[0] {
		require.Contains(t, w.Body.String(), `"code":-32602`)
		require.NotContains(t, w.Body.String(), `"effects"`)
		return nil
	}
	require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
	var response struct {
		Result struct {
			Effects []map[string]any `json:"effects"`
		} `json:"result"`
		Error any `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Nil(t, response.Error, w.Body.String())
	return response.Result.Effects
}

func ahpBoundaryEvent(kind string) map[string]any {
	e := ahpTestResponse()
	e["type"] = kind
	switch kind {
	case "tool.after":
		e = ahpToolEvent(kind)
		e["execution"] = map[string]any{"status": "executed"}
		e["outcome"] = "ok"
		e["items"] = []any{ahpTestMessage("tool", "result evidence")}
	case "model.request.before":
		delete(e, "execution")
		delete(e, "finishReason")
		delete(e, "usage")
		e["params"] = map[string]any{}
		e["items"] = []any{ahpTestMessage("tool", "result evidence")}
	case "user.message.outbound":
		e = ahpTestInbound()
		e["type"] = kind
		e["message"] = map[string]any{"channel": "cli", "messages": []any{ahpTestMessage("assistant", "outbound evidence")}}
	case "tool.permission.request":
		e = ahpToolEvent(kind)
		e["sandboxBypass"] = false
		e["suggestions"] = []any{}
	case "context.compact.before":
		e = ahpToolEvent(kind)
		delete(e, "tool")
		delete(e, "call")
		delete(e, "path")
		e["trigger"] = "auto"
		e["items"] = []any{ahpTestMessage("tool", "result evidence")}
	case "context.compact.after":
		e = ahpToolEvent(kind)
		delete(e, "tool")
		delete(e, "call")
		delete(e, "path")
		e["execution"] = map[string]any{"status": "executed"}
		e["summary"] = []any{ahpTestPart("summary evidence")}
		e["removed"] = []any{}
	}
	return e
}

func TestAHPContentBoundaryMatrix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ event, target, kind string }{
		{"tool.after", "output", message.ToolResponse}, {"model.response.after", "response", message.Assistant},
		{"model.request.before", "", message.ToolResponse}, {"user.message.outbound", "", message.Assistant},
		{"tool.permission.request", "", message.ToolRequest}, {"context.compact.before", "", message.ToolResponse}, {"context.compact.after", "summary", message.Assistant},
	} {
		t.Run(tc.event, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
			h, err := ti.service.ahpHandler()
			require.NoError(t, err)
			for _, variant := range []string{"blocked", "clean", "absent", "observe", "family-only", "nested-only", "merge-only"} {
				t.Run(variant, func(t *testing.T) { // Variants replace the scanner on their shared service and must run sequentially.
					scanner := &ahpEvidenceScanner{}
					if variant != "clean" {
						scanner.result = &risk.ScanResult{Action: "block", PolicyName: "Example policy"}
					}
					ti.service.riskScanner = scanner
					caps := map[string]any{"effects": []string{}}
					if tc.target == "" {
						if variant != "absent" {
							caps["effects"] = []string{"deny"}
						}
					} else {
						if variant != "absent" && variant != "nested-only" {
							caps["effects"] = []string{"modify"}
						}
						if variant != "absent" && variant != "family-only" {
							caps["modify"] = map[string]any{tc.target: map[string]any{"replace": variant != "merge-only", "merge": true}}
						}
					}
					e := ahpBoundaryEvent(tc.event)
					if variant == "observe" {
						w := ahpRequest(t, h, "hooks/observe", e, nil, ahpAuthHeaders)
						require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
						require.Empty(t, scanner.requests)
						return
					}
					effects := ahpBoundaryRequest(t, h, e, caps, tc.target != "" && (variant == "family-only" || variant == "nested-only"))
					enforce := variant != "absent" && (tc.target == "" || (variant != "family-only" && variant != "nested-only" && variant != "merge-only"))
					require.Equal(t, enforce, len(scanner.requests) > 0)
					if enforce {
						require.Equal(t, tc.kind, scanner.requests[0].MessageType)
						if tc.event == "tool.after" {
							require.Equal(t, "result evidence", scanner.requests[0].Text)
							require.NotContains(t, scanner.requests[0].Text, "example.txt")
						}
					}
					if enforce && variant != "clean" {
						require.Len(t, effects, 1)
						if tc.target == "" {
							require.Equal(t, "deny", effects[0]["type"])
						} else {
							require.Equal(t, "modify", effects[0]["type"])
							require.Equal(t, tc.target, effects[0]["target"])
							require.Equal(t, "replace", effects[0]["operation"])
							require.Empty(t, effects[0]["value"])
						}
					} else {
						require.Empty(t, effects)
					}
				})
			}
		})
	}
}

func TestAHPSkippedSuppliedOutput(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	scanner := &ahpEvidenceScanner{stubResultScanner: stubResultScanner{result: &risk.ScanResult{Action: "block", PolicyName: "Example policy"}}}
	ti.service.riskScanner = scanner
	e := ahpBoundaryEvent("tool.after")
	e["execution"] = map[string]any{"status": "skipped", "reason": "policy"}
	e["outcome"] = "denied"
	effects := ahpBoundaryRequest(t, h, e, map[string]any{"effects": []string{"modify"}, "modify": map[string]any{"output": map[string]any{"replace": true, "merge": false}}})
	require.Len(t, effects, 1)
	require.Equal(t, "result evidence", scanner.requests[0].Text)
	require.Equal(t, message.ToolResponse, scanner.requests[0].MessageType)
}

func TestAHPBoundaryEvidenceRoles(t *testing.T) {
	t.Parallel()
	raw := []any{ahpTestMessage("system", "trusted instruction"), ahpTestMessage("developer", "trusted instruction"), ahpTestMessage("user", "user evidence"), ahpTestMessage("tool", "tool evidence"), ahpTestMessage("assistant", "assistant evidence")}
	call := ahpTestPart("call arguments")
	call["category"] = "tool-call"
	reasoning := ahpTestPart("reasoning evidence")
	reasoning["category"] = "reasoning"
	assistant, ok := raw[4].(map[string]any)
	require.True(t, ok)
	assistant["parts"] = []any{ahpTestPart("assistant evidence"), call, reasoning}
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	var items []*ahp.ModelVisibleItem
	require.NoError(t, json.Unmarshal(b, &items))
	scans := ahpItemScans(items)
	require.Equal(t, []ahpBoundaryScan{{text: "user evidence", kind: message.User}, {text: "tool evidence", kind: message.ToolResponse}, {text: "assistant evidence", kind: message.Assistant}, {text: "call arguments", kind: message.ToolRequest}, {text: "reasoning evidence", kind: message.Assistant}}, scans)
	// Unavailable content does not silently fall back to tool input or metadata.
	require.Empty(t, ahpItemScans([]*ahp.ModelVisibleItem{nil, {Role: "tool", Parts: nil}}))
	require.Empty(t, ahpTextParts(nil))
}

func TestAHPMixedBoundaryOperationIdentityReplay(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	scanner := &ahpEvidenceScanner{}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	event := ahpBoundaryEvent("model.request.before")
	event["items"] = []any{ahpTestMessage("user", "user evidence"), ahpTestMessage("tool", "tool evidence"), ahpTestMessage("assistant", "assistant evidence"), ahpTestMessage("user", "later user evidence")}
	caps := map[string]any{"effects": []string{"deny"}}
	for range 2 {
		require.Empty(t, ahpBoundaryRequest(t, h, event, caps))
	}
	require.Len(t, scanner.requests, 6)
	identities := make(map[string]bool)
	for i, kind := range []string{message.User, message.ToolResponse, message.Assistant} {
		first, replay := scanner.requests[i], scanner.requests[i+3]
		require.Equal(t, kind, first.MessageType)
		require.Equal(t, kind, first.Provenance.MessageType)
		require.NotEmpty(t, first.Provenance.OperationID)
		require.False(t, identities[first.Provenance.OperationID], "different policy types must not share an operation")
		identities[first.Provenance.OperationID] = true
		require.Equal(t, first.Provenance.OperationID, replay.Provenance.OperationID)
		require.Equal(t, first.Text, replay.Text)
	}
	require.Equal(t, "user evidence\nlater user evidence", scanner.requests[0].Text)
}

type ahpContentMatchingScanner struct {
	stubResultScanner
	requests []risk.RealtimeScanRequest
}

func (s *ahpContentMatchingScanner) ScanForEnforcement(_ context.Context, req risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	s.requests = append(s.requests, req)
	if strings.Contains(req.Text, "RESTRICTED_TEST_CONTENT") {
		return &risk.ScanResult{Action: "block", PolicyName: "Example content policy"}, nil
	}
	return nil, nil
}

func TestAHPLaterOutputItemReplacesWholeOutput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ event, target, role string }{
		{"tool.after", "output", "tool"}, {"model.response.after", "response", "assistant"},
	} {
		t.Run(tc.event, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
			scanner := &ahpContentMatchingScanner{}
			ti.service.riskScanner = scanner
			h, err := ti.service.ahpHandler()
			require.NoError(t, err)
			caps := map[string]any{"effects": []string{"modify"}, "modify": map[string]any{tc.target: map[string]any{"replace": true, "merge": false}}}
			clean := ahpBoundaryEvent(tc.event)
			clean["items"] = []any{ahpTestMessage(tc.role, "safe first item"), ahpTestMessage(tc.role, "safe later item")}
			require.Empty(t, ahpBoundaryRequest(t, h, clean, caps))
			blocked := ahpBoundaryEvent(tc.event)
			blocked["items"] = []any{ahpTestMessage(tc.role, "safe first item"), ahpTestMessage(tc.role, "RESTRICTED_TEST_CONTENT"), ahpTestMessage(tc.role, "safe final item")}
			effects := ahpBoundaryRequest(t, h, blocked, caps)
			require.Len(t, scanner.requests, 2)
			require.Equal(t, "safe first item\nRESTRICTED_TEST_CONTENT\nsafe final item", scanner.requests[1].Text)
			require.Len(t, effects, 1)
			require.Equal(t, "modify", effects[0]["type"])
			require.Equal(t, tc.target, effects[0]["target"])
			require.Equal(t, "replace", effects[0]["operation"])
			// The entire original output list is removed, not just the matched part.
			require.Equal(t, []any{}, effects[0]["value"])
		})
	}
}

func TestAHPQuarantineBlocksEmptyBoundary(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"tool.after", "model.request.before", "model.response.after", "user.message.outbound", "context.compact.before", "context.compact.after", "tool.permission.request"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			auth := hookAuthContext(t, ctx)
			ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
			scanner := &ahpEvidenceScanner{}
			ti.service.riskScanner = scanner
			h, err := ti.service.ahpHandler()
			require.NoError(t, err)
			event := ahpBoundaryEvent(kind)
			event["items"] = []any{}
			if kind == "context.compact.before" {
				event["instructions"] = []any{ahpTestPart("Summarize the conversation.")}
			}
			if kind == "user.message.outbound" {
				event["message"] = map[string]any{"channel": "cli", "messages": []any{ahpTestMessage("assistant", "")}}
			}
			if kind == "context.compact.after" {
				event["summary"] = []any{ahpTestPart("")}
			}
			caps := map[string]any{"effects": []string{"deny"}}
			target := ""
			switch kind {
			case "tool.after":
				target = "output"
			case "model.response.after":
				target = "response"
			case "context.compact.after":
				target = "summary"
			}
			if target != "" {
				caps = map[string]any{"effects": []string{"modify"}, "modify": map[string]any{target: map[string]any{"replace": true, "merge": false}}}
			}
			sessionEnvelope, ok := event["session"].(map[string]any)
			require.True(t, ok)
			session, ok := sessionEnvelope["id"].(string)
			require.True(t, ok)
			require.NotNil(t, ti.service.cache)
			require.NoError(t, sessionquarantine.Write(ctx, ti.service.cache, sessionquarantine.Quarantine{
				OrganizationID: auth.ActiveOrganizationID, ProjectID: auth.ProjectID.String(), SessionID: session,
				RiskPolicyID: "", RiskPolicyName: "Example quarantine policy", Reason: "test quarantine", CreatedAt: time.Now().UTC(),
			}))
			effects := ahpBoundaryRequest(t, h, event, caps)
			require.Len(t, effects, 1)
			if target == "" {
				require.Equal(t, "deny", effects[0]["type"])
				require.Contains(t, effects[0]["reason"], "quarantined")
			} else {
				require.Equal(t, "modify", effects[0]["type"])
				require.Equal(t, target, effects[0]["target"])
				require.Empty(t, effects[0]["value"])
			}
			require.Empty(t, scanner.requests, "quarantine must run even without scannable evidence")
		})
	}
}

func TestAHPMergeBoundaryScans(t *testing.T) {
	t.Parallel()
	require.Empty(t, mergeAHPScans(nil))
	require.Empty(t, mergeAHPScans([]ahpBoundaryScan{}))
	scans := mergeAHPScans([]ahpBoundaryScan{
		{text: "", kind: message.User, tool: ""},
		{text: "first", kind: message.User, tool: ""},
		{text: "", kind: message.User, tool: ""},
		{text: "second", kind: message.User, tool: ""},
		{text: "", kind: message.ToolResponse, tool: "read_file"},
	})
	require.Equal(t, []ahpBoundaryScan{
		{text: "first\nsecond", kind: message.User, tool: ""},
		{text: "", kind: message.ToolResponse, tool: "read_file"},
	}, scans)
}

func TestAHPCompactionAbsentInstructions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	scanner := &ahpEvidenceScanner{}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	event := ahpBoundaryEvent("context.compact.before")
	event["items"] = []any{ahpTestMessage("user", "context evidence")}
	require.Empty(t, ahpBoundaryRequest(t, h, event, map[string]any{"effects": []string{"deny"}}))
	require.Len(t, scanner.requests, 1)
	require.Equal(t, "context evidence", scanner.requests[0].Text)
	require.Equal(t, message.User, scanner.requests[0].MessageType)
}

func TestAHPContentBoundaryIntentDoesNotDependOnScans(t *testing.T) {
	t.Parallel()
	for _, eventType := range []string{"notification.reported", "usage.reported"} {
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			auth := hookAuthContext(t, ctx)
			sessionID := "boundary-session"
			require.NoError(t, sessionquarantine.Write(ctx, ti.service.cache, sessionquarantine.Quarantine{
				OrganizationID: auth.ActiveOrganizationID, ProjectID: auth.ProjectID.String(), SessionID: sessionID,
				RiskPolicyID: "", RiskPolicyName: "Example policy", Reason: "test quarantine", CreatedAt: time.Now().UTC(),
			}))
			for name, scans := range map[string][]ahpBoundaryScan{"nil": nil, "empty": {}} {
				payload := canonicalIngestPayload("example-harness", eventType, sessionID)
				payload.IdempotencyKey = new(eventType + ":" + name)
				reason, err := ti.service.ingestAHPBoundary(ctx, payload, false, scans...)
				require.NoError(t, err)
				require.Contains(t, reason, "quarantined", "nil and empty evidence must both retain boundary intent")
			}
		})
	}
}

func TestAHPCompactionInstructionsAreNotUserEvidence(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"user", "tool", "instructions-only"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
			scanner := &ahpEvidenceScanner{}
			scanner.result = &risk.ScanResult{Action: "block", PolicyName: "Example policy"}
			ti.service.riskScanner = scanner
			h, err := ti.service.ahpHandler()
			require.NoError(t, err)
			event := ahpBoundaryEvent("context.compact.before")
			event["instructions"] = []any{ahpTestPart("Summarize the conversation.")}
			event["items"] = []any{}
			if role != "instructions-only" {
				event["items"] = []any{ahpTestMessage(role, "context evidence")}
			}
			effects := ahpBoundaryRequest(t, h, event, map[string]any{"effects": []string{"deny"}})
			if role == "instructions-only" {
				require.Empty(t, scanner.requests)
				require.Empty(t, effects)
				return
			}
			require.Len(t, scanner.requests, 1)
			require.Equal(t, "context evidence", scanner.requests[0].Text)
			kind := message.User
			if role == "tool" {
				kind = message.ToolResponse
			}
			require.Equal(t, kind, scanner.requests[0].MessageType)
			require.Len(t, effects, 1)
			require.Equal(t, "deny", effects[0]["type"])
		})
	}
}
