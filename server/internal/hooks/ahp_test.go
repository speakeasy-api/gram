package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"
)

func ahpTestEvent(eventType string) map[string]any {
	return map[string]any{"id": "event-1", "source": "https://custom-harness.example/runtime", "type": eventType, "time": "2026-08-24T08:51:14Z", "session": map[string]any{"id": "shared-session"}, "tool": map[string]any{"name": "read_file", "kind": "file_read", "input": map[string]any{"path": "example.txt"}, "origin": "native"}, "call": map[string]any{"id": "call-1"}, "path": "example"}
}

func TestAHPNormalizationIsolationAndProvenance(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	auth := &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project}
	event := ahpTestEvent("tool.before")
	p, a, err := normalizeAHPEvent(event, auth, "intercept")
	require.NoError(t, err)
	require.Equal(t, "tool.requested", p.Event.Type)
	require.Equal(t, event["source"], p.Source.Adapter)
	require.Equal(t, "ahp", a[attr.Key("gram.hook.transport")])
	require.Equal(t, "intercept", a[attr.Key("gram.hook.mode")])
	base := hookTelemetryBaseAttrs(p, auth, telemetryHookEventName(p), p.Source.Adapter)
	require.Equal(t, "hook.ingest.v1", base[attr.Key("gram.hook.schema")])
	require.Equal(t, "tool.requested", base[attr.Key("gram.hook.canonical_event")])
	for _, change := range []string{"org", "project", "source"} {
		copyAuth := *auth
		copyEvent := ahpTestEvent("tool.before")
		switch change {
		case "org":
			copyAuth.ActiveOrganizationID = "org_other"
		case "project":
			id := uuid.New()
			copyAuth.ProjectID = &id
		case "source":
			copyEvent["source"] = "urn:harness:other"
		}
		other, _, err := normalizeAHPEvent(copyEvent, &copyAuth, "observe")
		require.NoError(t, err)
		require.NotEqual(t, *p.IdempotencyKey, *other.IdempotencyKey)
		require.NotEqual(t, *p.Session.ID, *other.Session.ID)
	}
	delete(event, "session")
	delete(event, "call")
	p, _, err = normalizeAHPEvent(event, auth, "observe")
	require.NoError(t, err)
	require.Nil(t, p.Session)
	require.Empty(t, *p.Data.ToolCall.ID)
	require.Equal(t, "https://harness.example/path", ahpSourceLabel("https://secret@harness.example/path?token=secret#secret"))
	require.LessOrEqual(t, len(ahpSourceLabel("urn:harness:"+strings.Repeat("x", 1000))), 256)
}

func TestAHPNormalizationOutcomesAndUsage(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	auth := &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project}
	for _, test := range []struct{ status, outcome, canonical string }{{"executed", "ok", "tool.completed"}, {"executed", "error", "tool.failed"}, {"executed", "denied", "tool.skipped"}, {"skipped", "denied", "tool.skipped"}, {"skipped", "ok", "tool.skipped"}} {
		e := ahpTestEvent("tool.after")
		e["execution"] = map[string]any{"status": test.status}
		e["outcome"] = test.outcome
		e["output"] = "not a protocol output"
		e["items"] = []any{map[string]any{"selection": "body", "body": map[string]any{"ref": "private-ref"}}}
		p, a, err := normalizeAHPEvent(e, auth, "observe")
		require.NoError(t, err)
		require.Equal(t, test.canonical, p.Event.Type)
		require.Equal(t, test.outcome, a[attr.Key("gram.hook.outcome")])
		require.Nil(t, p.Data.ToolCall.Output)
		if test.canonical == "tool.skipped" {
			require.NotContains(t, telemetryHookEventName(p), "PostToolUse")
		}
	}
	e := ahpTestEvent("model.error")
	e["model"] = map[string]any{"id": "example-model"}
	e["execution"] = map[string]any{"status": "skipped"}
	e["usage"] = map[string]any{"kind": "amount", "scope": "attempt", "completeness": "partial", "provenance": "provider", "inputTokens": 12, "cost": map[string]any{"amount": 0.01, "currency": "USD", "basis": "reported"}}
	p, a, err := normalizeAHPEvent(e, auth, "observe")
	require.NoError(t, err)
	require.Equal(t, "usage.reported", p.Event.Type)
	require.Equal(t, "model_attempt", a[attr.Key("gram.hook.usage_authority")])
	require.Equal(t, 12, *p.Data.Usage.InputTokens)
	require.InDelta(t, 0.01, *p.Data.Usage.Cost, 0.000001)
	e["type"] = "user.message.outbound"
	p, _, err = normalizeAHPEvent(e, auth, "observe")
	require.NoError(t, err)
	require.Nil(t, p.Data.Usage)
	e["type"] = "task.change.after"
	p, _, err = normalizeAHPEvent(e, auth, "observe")
	require.NoError(t, err)
	require.Equal(t, "ahp.task.change.after", p.Event.Type)
	require.Nil(t, p.Raw)
	require.Nil(t, p.Data.ToolCall.Input)
}

type ahpTestAuthorizer struct {
	authCtx        *contextvalues.AuthContext
	keys           []string
	schemes        []string
	projects       map[string]uuid.UUID
	boundProjectID *uuid.UUID
}

func (a *ahpTestAuthorizer) Authorize(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	a.keys = append(a.keys, key)
	a.schemes = append(a.schemes, scheme.Name)
	if scheme.Name == constants.KeySecurityScheme {
		if key != "example" {
			return ctx, errors.New("invalid key")
		}
		authCopy := *a.authCtx
		authCopy.ProjectID = nil
		return contextvalues.SetAuthContext(ctx, &authCopy), nil
	}
	if scheme.Name != constants.ProjectSlugSecuritySchema {
		return ctx, errors.New("unexpected scheme")
	}
	projects := a.projects
	if len(projects) == 0 {
		projects = map[string]uuid.UUID{"chosen": *a.authCtx.ProjectID}
	}
	var project uuid.UUID
	if key == "" {
		if len(projects) != 1 {
			return ctx, errors.New("project selection required")
		}
		for _, id := range projects {
			project = id
		}
	} else {
		var ok bool
		project, ok = projects[key]
		if !ok {
			return ctx, errors.New("unknown project")
		}
	}
	if a.boundProjectID != nil && project != *a.boundProjectID {
		return ctx, errors.New("key project binding mismatch")
	}
	authCopy := *a.authCtx
	authCopy.ProjectID = &project
	return contextvalues.SetAuthContext(ctx, &authCopy), nil
}

func TestAHPHandlerAuthenticationCapabilitiesAndValidation(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	auth := &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project}
	for _, tc := range []struct {
		name, key, bearer, project string
		multiple, bound            bool
		status                     int
	}{{"missing", "", "", "", false, false, 401}, {"bearer single project", "", "Bearer example", "", false, false, 200}, {"gram headers", "example", "", "chosen", false, false, 200}, {"multi project omitted", "example", "", "", true, false, 401}, {"conflict", "other", "Bearer example", "chosen", false, false, 401}, {"multi project chosen", "example", "", "chosen", true, false, 200}, {"bound key chosen", "example", "", "chosen", true, true, 200}, {"bound key other", "example", "", "other", true, true, 401}, {"unknown project", "example", "", "unknown", false, false, 401}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projects := map[string]uuid.UUID{"chosen": project}
			if tc.multiple {
				projects["other"] = uuid.New()
			}
			a := &ahpTestAuthorizer{authCtx: auth, projects: projects}
			if tc.bound {
				a.boundProjectID = &project
			}
			s := &Service{auth: a}
			h, err := s.ahpHandler()
			require.NoError(t, err)
			r := httptest.NewRequest("POST", "/ahp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"hooks/capabilities","params":{"protocolVersion":"draft"}}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Gram-Key", tc.key)
			r.Header.Set("Authorization", tc.bearer)
			r.Header.Set("Gram-Project", tc.project)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == 200 {
				require.True(t, ahp.ParseCapabilitiesResponse(w.Body.Bytes()).OK, w.Body.String())
				require.Equal(t, []string{"example", tc.project}, a.keys)
				require.Equal(t, []string{constants.KeySecurityScheme, constants.ProjectSlugSecuritySchema}, a.schemes)
			}
		})
	}
}

func TestAHPHandlerInterceptEffectsAndObserveSafety(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	ti.service.productFeatures = staticFeatures{failOpen: false}
	scanner := &stubResultScanner{result: &risk.ScanResult{Action: "warn", PolicyName: "Example warning"}}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	for _, tc := range []struct {
		method   string
		effects  []string
		decision string
	}{{"hooks/intercept", []string{"deny"}, "deny"}, {"hooks/intercept", []string{}, ""}, {"hooks/observe", nil, ""}} {
		event := ahpTestEvent("tool.before")
		event["id"] = uuid.NewString()
		params := map[string]any{"protocolVersion": "draft", "event": event}
		request := map[string]any{"jsonrpc": "2.0", "method": tc.method, "params": params}
		if tc.method == "hooks/intercept" {
			params["capabilities"] = map[string]any{"effects": tc.effects}
			request["id"] = event["id"]
		}
		b, err := json.Marshal(request)
		require.NoError(t, err)
		r := httptest.NewRequest("POST", "/ahp", strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if tc.method == "hooks/observe" {
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
		} else {
			require.Equal(t, 200, w.Code)
			require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())
			var response struct {
				Result struct {
					Effects []map[string]any `json:"effects"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			if tc.decision != "" {
				require.Len(t, response.Result.Effects, 1)
				require.Equal(t, "deny", response.Result.Effects[0]["type"])
			} else {
				require.Empty(t, response.Result.Effects)
			}
		}
	}
	require.False(t, scanner.recordedChallenge)
}

type ahpErrorFeatures struct{}

func (ahpErrorFeatures) IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error) {
	return false, errors.New("unavailable")
}

func TestAHPSharedProcessorFailPolicyAndMCP(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	for _, open := range []bool{true, false} {
		ti.service.productFeatures = staticFeatures{failOpen: open}
		ti.service.riskScanner = &stubResultScanner{result: analysisUnavailableResult("block")}
		result, err := ti.service.ingestAHP(ctx, ahpTestEvent("tool.before"), "intercept", true)
		require.NoError(t, err)
		if open {
			require.Equal(t, "allow", result.Decision)
		} else {
			require.Equal(t, "deny", result.Decision)
		}
		// Missing prompt references follow the same posture, never scan an empty body.
		promptEvent := ahpTestEvent("turn.start")
		promptEvent["trigger"] = "user"
		result, err = ti.service.ingestAHP(ctx, promptEvent, "intercept", true)
		require.NoError(t, err)
		if open {
			require.Equal(t, "allow", result.Decision)
		} else {
			require.Equal(t, "deny", result.Decision)
		}
	}
	ti.service.productFeatures = ahpErrorFeatures{}
	require.False(t, ti.service.ahpFailOpen(ctx))
	ti.service.productFeatures = staticFeatures{failOpen: true}
	ti.service.riskScanner = &stubResultScanner{shadowPolicy: &risk.ShadowMCPPolicy{ID: uuid.NewString(), Name: "Managed only", Disposition: "block_all"}}
	e := ahpTestEvent("tool.before")
	tool, ok := e["tool"].(map[string]any)
	require.True(t, ok)
	tool["mcp"] = map[string]any{"server": map[string]any{"id": "unknown", "name": "Example"}, "toolName": "read_file", "provenance": "runtime", "connection": map[string]any{"transport": "http"}}
	result, err := ti.service.ingestAHP(ctx, e, "intercept", true)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision, "unknown per-occurrence MCP evidence is not an empty inventory")
	// Metadata source carries tenant identity solely from authentication.
	p, _, err := normalizeAHPEvent(e, auth, "observe")
	require.NoError(t, err)
	require.Nil(t, p.Data.McpInventoryCollected)
}

func TestAHPSharedProcessorObserveAndCapabilitySpendGate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	scanner := &recordingRiskScanner{}
	ti.service.riskScanner = scanner
	event := ahpTestEvent("tool.before")
	result, err := ti.service.ingestAHP(ctx, event, "observe", false)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
	require.Zero(t, scanner.scans)
	// A capable arbitrary URI, unlike the legacy adapter allowlist, is spend gated.
	seedSpendBlock(t, ctx, ti, auth.ActiveOrganizationID, *auth.Email)
	result, err = ti.service.ingestAHP(ctx, event, "intercept", true)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.Contains(t, *result.Message, "spend rule")
	require.Zero(t, scanner.scans)
	result, err = ti.service.ingestAHP(ctx, event, "intercept", false)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
}

func TestAHPPrincipalNamespaceAndMissingQuarantineCache(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	event := ahpTestEvent("tool.before")
	first, _, err := normalizeAHPEventWithContent(event, auth, "intercept", nil, "principal-one")
	require.NoError(t, err)
	second, _, err := normalizeAHPEventWithContent(event, auth, "intercept", nil, "principal-two")
	require.NoError(t, err)
	require.NotEqual(t, *first.Session.ID, *second.Session.ID)
	require.NotEqual(t, *first.IdempotencyKey, *second.IdempotencyKey)
	require.NotEqual(t, *first.Data.ToolCall.ID, *second.Data.ToolCall.ID)
	afterEvent := ahpTestEvent("tool.after")
	afterEvent["outcome"] = "ok"
	afterEvent["execution"] = map[string]any{"status": "executed"}
	after, _, err := normalizeAHPEventWithContent(afterEvent, auth, "observe", nil, "principal-one")
	require.NoError(t, err)
	require.Equal(t, *first.Data.ToolCall.ID, *after.Data.ToolCall.ID)
	ti.service.riskScanner = &recordingRiskScanner{}
	scoped, _, err := normalizeAHPEventWithContent(event, auth, "intercept", nil, ahpPrincipalIdentity(ctx, auth))
	require.NoError(t, err)
	ti.service.cache = mcpGetErrorCache{Cache: ti.service.cache, failKey: fmt.Sprintf("session:quarantine:%s:%s:%s", auth.ActiveOrganizationID, auth.ProjectID.String(), *scoped.Session.ID), err: errors.New("cache unavailable")}
	for _, open := range []bool{true, false} {
		ti.service.productFeatures = staticFeatures{failOpen: open}
		features := productfeaturesrepo.New(ti.conn)
		if open {
			_, err = features.EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureHooksFailOpen)})
		} else {
			_, err = features.DeleteFeature(ctx, productfeaturesrepo.DeleteFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureHooksFailOpen)})
		}
		require.NoError(t, err)
		result, err := ti.service.ingestAHP(ctx, event, "intercept", true)
		require.NoError(t, err)
		if open {
			require.Equal(t, "allow", result.Decision)
		} else {
			require.Equal(t, "deny", result.Decision)
		}
		attrs := map[attr.Key]any{}
		tracked, failure := withAHPFailureTracker(context.WithValue(ctx, authenticatedIngestOptionsKey{}, AuthenticatedIngestOptions{AHPPolicy: true, SourceAttributes: attrs}))
		ti.service.checkQuarantineGate(tracked, canonicalHookEvent(scoped, auth, canonicalActor{UserID: auth.UserID}, ti.service.now()))
		require.Equal(t, "quarantine_gate_unavailable", *failure)
		require.Equal(t, *failure, attrs[attr.Key("gram.hook.enforcement_gap")])
	}

}

func TestAHPValidModelUsageAndErrorRedaction(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	auth := &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project, UserID: "user_example"}
	for _, eventType := range []string{"model.response.after", "model.error"} {
		e := map[string]any{"id": "event-1", "source": "urn:example:harness", "type": eventType, "time": "2026-09-15T12:00:00Z", "model": map[string]any{"id": "example", "provider": "example"}, "attempt": map[string]any{"id": "attempt-1", "number": 1}, "execution": map[string]any{"status": "executed"}, "usage": map[string]any{"kind": "amount", "scope": "attempt", "completeness": "partial", "provenance": "provider", "inputTokens": 12, "cost": map[string]any{"amount": 0.01, "currency": "USD", "basis": "reported"}}}
		if eventType == "model.response.after" {
			e["items"] = []any{}
			e["finishReason"] = "stop"
		} else {
			e["error"] = map[string]any{"class": "operation", "message": "failed"}
		}
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "hooks/observe", "params": map[string]any{"protocolVersion": "draft", "event": e}})
		require.NoError(t, err)
		parsed := ahp.ParseObserveNotification(wire)
		require.True(t, parsed.OK, parsed.Diagnostics)
		p, a, err := normalizeAHPEvent(parsed.Value.Params.Event, auth, "observe")
		require.NoError(t, err)
		require.NotNil(t, p.Data.Usage)
		require.Equal(t, "attempt", a[attr.Key("gram.hook.usage_scope")])
		require.Equal(t, "partial", a[attr.Key("gram.hook.usage_completeness")])
		require.Equal(t, "provider", a[attr.Key("gram.hook.usage_provenance")])
		require.Equal(t, "reported", a[attr.Key("gram.hook.cost_basis")])
	}
	e := ahpTestEvent("tool.after")
	e["outcome"] = "error"
	e["execution"] = map[string]any{"status": "executed"}
	e["items"] = []any{}
	e["error"] = map[string]any{"class": "operation", "message": "failed", "native": map[string]any{"secret": "PRIVATE_NATIVE_MARKER"}}
	wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "hooks/observe", "params": map[string]any{"protocolVersion": "draft", "event": e}})
	require.NoError(t, err)
	parsed := ahp.ParseObserveNotification(wire)
	require.True(t, parsed.OK, parsed.Diagnostics)
	p, _, err := normalizeAHPEvent(parsed.Value.Params.Event, auth, "observe")
	require.NoError(t, err)
	encoded, err := json.Marshal(p.Data.ToolCall.Error)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_NATIVE_MARKER")
}

func TestAHPMCPAddressRedactionAndStdioIdentity(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	auth := &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project, UserID: "user_example"}
	normalize := func(connection map[string]any) *gen.IngestPayload {
		event := ahpTestEvent("tool.before")
		tool, ok := event["tool"].(map[string]any)
		require.True(t, ok)
		tool["origin"] = "mcp"
		tool["mcp"] = map[string]any{"server": map[string]any{"id": "server-1", "name": "Example"}, "toolName": "read_file", "provenance": "runtime", "connection": connection}
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": event["id"], "method": "hooks/intercept", "params": map[string]any{"protocolVersion": "draft", "event": event, "capabilities": map[string]any{"effects": []string{"deny"}}}})
		require.NoError(t, err)
		parsed := ahp.ParseInterceptRequest(wire)
		require.True(t, parsed.OK, parsed.Diagnostics)
		p, _, err := normalizeAHPEvent(parsed.Value.Params.Event, auth, "intercept")
		require.NoError(t, err)
		return p
	}
	p := normalize(map[string]any{"transport": "http", "url": "https://secret-user:PRIVATE_CREDENTIAL_MARKER@mcp.example/tools?token=PRIVATE_CREDENTIAL_MARKER"})
	require.NotContains(t, *p.Data.Mcp.URL, "PRIVATE_CREDENTIAL_MARKER")
	first := normalize(map[string]any{"transport": "stdio", "command": "npx", "cwd": "/tmp/example", "args": []string{"-y", "@example/server-one", "--token", "PRIVATE_CREDENTIAL_MARKER"}})
	second := normalize(map[string]any{"transport": "stdio", "command": "npx", "cwd": "/tmp/example", "args": []string{"-y", "@example/server-two"}})
	require.NotEqual(t, *first.Data.Mcp.Command, *second.Data.Mcp.Command)
	require.NotContains(t, *first.Data.Mcp.Command, "PRIVATE_CREDENTIAL_MARKER")
}
