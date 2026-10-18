package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// ahpTestAuthorizer accepts the key "example" for the project slug "chosen".
type ahpTestAuthorizer struct {
	authCtx *contextvalues.AuthContext
}

func (a *ahpTestAuthorizer) Authorize(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	switch {
	case scheme.Name == constants.KeySecurityScheme && key == "example":
		authCopy := *a.authCtx
		authCopy.ProjectID = nil
		return contextvalues.SetAuthContext(ctx, &authCopy), nil
	case scheme.Name == constants.ProjectSlugSecuritySchema && key == "chosen":
		authCopy := *a.authCtx
		return contextvalues.SetAuthContext(ctx, &authCopy), nil
	default:
		return ctx, errors.New("unauthorized")
	}
}

func ahpRequest(t *testing.T, h http.Handler, method string, event map[string]any, effects []string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	params := map[string]any{"protocolVersion": "draft", "event": event}
	body := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if method == "hooks/intercept" {
		params["capabilities"] = map[string]any{"effects": effects}
		body["id"] = event["id"]
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/ahp", strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func ahpToolEvent(eventType string) map[string]any {
	return map[string]any{
		"id":      uuid.NewString(),
		"source":  "https://harness.example/runtime",
		"type":    eventType,
		"time":    "2026-10-13T08:51:14Z",
		"harness": map[string]any{"name": "example-harness", "version": "1.2.3"},
		"session": map[string]any{"id": uuid.NewString()},
		"tool":    map[string]any{"name": "read_file", "kind": "file_read", "input": map[string]any{"path": "example.txt"}, "origin": "native"},
		"call":    map[string]any{"id": "call-1"},
		"path":    "example",
	}
}

var ahpAuthHeaders = map[string]string{"Authorization": "Bearer example", "Gram-Project": "chosen"}

func TestAHPAuthentication(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	s := &Service{auth: &ahpTestAuthorizer{authCtx: &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project}}}
	h, err := s.ahpHandler()
	require.NoError(t, err)

	for name, headers := range map[string]map[string]string{
		"missing key":     {"Gram-Project": "chosen"},
		"missing project": {"Authorization": "Bearer example"},
		"invalid key":     {"Authorization": "Bearer other", "Gram-Project": "chosen"},
		"unknown project": {"Gram-Key": "example", "Gram-Project": "other"},
	} {
		w := ahpRequest(t, h, "hooks/intercept", ahpToolEvent("tool.before"), []string{"deny"}, headers)
		require.Equal(t, http.StatusUnauthorized, w.Code, name)
	}
}

func TestAHPToolBeforeDeny(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		effects []string
		denied  bool
	}{
		"deny granted":     {effects: []string{"deny"}, denied: true},
		"deny not granted": {effects: []string{}, denied: false},
	} {
		scanner := &countingBlockScanner{stubResultScanner: stubResultScanner{result: &risk.ScanResult{Action: "block", PolicyName: "Example policy"}}}
		ti.service.riskScanner = scanner
		w := ahpRequest(t, h, "hooks/intercept", ahpToolEvent("tool.before"), tc.effects, ahpAuthHeaders)
		require.Equal(t, http.StatusOK, w.Code, name)
		require.Equal(t, tc.denied, scanner.scans > 0, "policy is evaluated only when the host can deny: %s", name)
		require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
		var response struct {
			Result struct {
				Effects []map[string]any `json:"effects"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		if tc.denied {
			require.Len(t, response.Result.Effects, 1, name)
			require.Equal(t, "deny", response.Result.Effects[0]["type"])
		} else {
			require.Empty(t, response.Result.Effects, name)
		}
	}
}

func TestAHPToolBeforeAllow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)

	for name, headers := range map[string]map[string]string{
		"bearer":           ahpAuthHeaders,
		"lowercase bearer": {"Authorization": "bearer example", "Gram-Project": "chosen"},
		"gram key":         {"Gram-Key": "example", "Gram-Project": "chosen"},
	} {
		w := ahpRequest(t, h, "hooks/intercept", ahpToolEvent("tool.before"), []string{"deny"}, headers)
		require.Equal(t, http.StatusOK, w.Code, name)
		require.NotContains(t, w.Body.String(), `"deny"`, name)
	}
}

// countingBlockScanner counts enforcement scans while returning a fixed result.
type countingBlockScanner struct {
	stubResultScanner
	scans int
}

func (s *countingBlockScanner) ScanForEnforcement(ctx context.Context, req risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	s.scans++
	return s.stubResultScanner.ScanForEnforcement(ctx, req)
}

func TestAHPObservationPersistsWithoutEvaluation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	auth := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)

	start := time.Now().Add(-time.Minute)
	event := ahpToolEvent("tool.before")
	event["time"] = time.Now().UTC().Format(time.RFC3339)
	w := ahpRequest(t, h, "hooks/observe", event, nil, ahpAuthHeaders)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Zero(t, scanner.scans)

	require.Eventually(t, func() bool {
		logs, err := chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: auth.ProjectID.String(),
			TimeStart:     start.UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) == 1
	}, 2*time.Second, 50*time.Millisecond, "expected the observed tool request row")
}

func TestAHPNotificationRetainsTurnID(t *testing.T) {
	t.Parallel()
	for _, eventType := range []string{
		"model.request.before",
		"user.message.outbound",
		"context.compact.before",
		"context.compact.after",
		"tool.permission.resolved",
		"tool.batch.after",
		"model.error",
		"turn.end",
	} {
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()
			ctx, ti, published := newTeeTestService(t)
			ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
			scanner := &recordingRiskScanner{}
			ti.service.riskScanner = scanner
			h, err := ti.service.ahpHandler()
			require.NoError(t, err)

			event := ahpBoundaryEvent(eventType)
			switch eventType {
			case "tool.permission.resolved":
				event = ahpToolEvent(eventType)
				event["decision"] = "allow"
				event["decidedBy"] = "user"
			case "tool.batch.after":
				event = ahpToolEvent(eventType)
				delete(event, "tool")
				delete(event, "call")
				delete(event, "path")
				event["batch"] = map[string]any{"id": uuid.NewString()}
				call := ahpToolEvent("tool.after")
				event["calls"] = []any{map[string]any{
					"call": call["call"], "tool": call["tool"], "path": call["path"],
					"execution": map[string]any{"status": "executed"}, "outcome": "ok",
				}}
			case "model.error":
				delete(event, "finishReason")
				delete(event, "items")
				event["error"] = map[string]any{"class": "provider", "message": "example error"}
			case "turn.end":
				delete(event, "usage")
				event["outcome"] = "completed"
				event["continuationCount"] = 0
			}
			turnID := uuid.NewString()
			event["turn"] = map[string]any{"id": turnID}
			event["time"] = time.Now().UTC().Format(time.RFC3339)

			w := ahpRequest(t, h, "hooks/observe", event, nil, ahpAuthHeaders)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			ti.service.otelTeeDrains.Wait()
			require.Zero(t, scanner.scans)
			records := published.records()
			require.Len(t, records, 1, "expected the actual observation handler to persist a notification")
			require.Equal(t, turnID, teeStringAttr(t, records[0], "gram.hook.turn_id"))
		})
	}
}

func TestAHPToolAfterPayload(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		execution map[string]any
		outcome   string
		eventType string
	}{
		"executed ok":    {execution: map[string]any{"status": "executed"}, outcome: "ok", eventType: "tool.completed"},
		"executed error": {execution: map[string]any{"status": "executed"}, outcome: "error", eventType: "tool.failed"},
		"skipped":        {execution: map[string]any{"status": "skipped", "reason": "policy"}, outcome: "denied", eventType: ""},
	} {
		raw := ahpToolEvent("tool.after")
		raw["execution"] = tc.execution
		raw["outcome"] = tc.outcome
		raw["items"] = []any{}
		b, err := json.Marshal(raw)
		require.NoError(t, err)
		var event ahp.ToolAfterEvent
		require.NoError(t, json.Unmarshal(b, &event), name)

		payload := ahpToolAfterPayload(&event)
		if tc.eventType == "" {
			require.Nil(t, payload, name)
			continue
		}
		require.NotNil(t, payload, name)
		require.Equal(t, tc.eventType, payload.Event.Type, name)
		require.Equal(t, "example-harness", payload.Source.Adapter)
		require.Equal(t, "1.2.3", *payload.Source.AdapterVersion)
		require.Equal(t, "read_file", *payload.Data.ToolCall.Name)
		require.Equal(t, map[string]any{"path": "example.txt"}, payload.Data.ToolCall.Input)
	}
}
