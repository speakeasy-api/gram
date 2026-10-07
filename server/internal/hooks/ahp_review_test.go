package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ahp "github.com/agenthooksprotocol/go-sdk"
	ahpserver "github.com/agenthooksprotocol/go-sdk/server"
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/stretchr/testify/require"
)

func TestAHPMissingSessionFollowsFailurePosture(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &recordingRiskScanner{}
	event := ahpTestEvent("tool.before")
	delete(event, "session")
	for _, open := range []bool{false, true} {
		ti.service.productFeatures = staticFeatures{failOpen: open}
		result, err := ti.service.ingestAHP(ctx, event, "intercept", true)
		require.NoError(t, err)
		if open {
			require.Equal(t, "allow", result.Decision)
		} else {
			require.Equal(t, "deny", result.Decision)
		}
		auth, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		p, attrs, err := normalizeAHPEvent(event, auth, "intercept")
		require.NoError(t, err)
		require.Nil(t, p.Session)
		tracked, failure := withAHPFailureTracker(context.WithValue(ctx, authenticatedIngestOptionsKey{}, AuthenticatedIngestOptions{AHPPolicy: true, SourceAttributes: attrs}))
		ti.service.checkQuarantineGate(tracked, canonicalHookEvent(p, auth, canonicalActor{UserID: auth.UserID}, ti.service.now()))
		require.Equal(t, "quarantine_session_unavailable", *failure)
		require.Equal(t, *failure, attrs[attr.Key("gram.hook.enforcement_gap")])
	}
	result, err := ti.service.ingestAHP(ctx, event, "observe", false)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
}

type ahpFailingContentCache struct {
	cache.Cache
	added, deleted string
}

func (c *ahpFailingContentCache) Add(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	added, err := c.Cache.Add(ctx, key, ttl)
	if added {
		c.added = key
	}
	if err != nil {
		return added, fmt.Errorf("add test admission: %w", err)
	}
	return added, nil
}
func (c *ahpFailingContentCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if strings.HasPrefix(key, "ahp:content:v1:") {
		return errors.New("write unavailable")
	}
	if err := c.Cache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("set test cache: %w", err)
	}
	return nil
}
func (c *ahpFailingContentCache) Delete(ctx context.Context, key string) error {
	c.deleted = key
	if err := c.Cache.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete test cache: %w", err)
	}
	return nil
}

func TestAHPFailedContentWriteReleasesAdmission(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	c := &ahpFailingContentCache{Cache: ti.service.cache}
	ti.service.cache = c
	data := []byte("example")
	r := httptest.NewRequest("POST", "/ahp/content", bytes.NewReader(data)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("Content-Length", fmt.Sprint(len(data)))
	r.Header.Set("AHP-Content-SHA256", fmt.Sprintf("%x", sha256.Sum256(data)))
	w := httptest.NewRecorder()
	ti.service.ahpUploadContent(w, r)
	require.Equal(t, 503, w.Code)
	require.NotEmpty(t, c.added)
	require.Equal(t, c.added, c.deleted)
	won, err := c.Cache.Add(ctx, c.added, ahpContentTTL)
	require.NoError(t, err)
	require.True(t, won, "failed content publication must not consume quota")
}

type ahpFailingShadowScanner struct{ stubResultScanner }

func (*ahpFailingShadowScanner) LookupShadowMCPBlockingPolicy(context.Context, string, uuid.UUID, string) (*risk.ShadowMCPPolicy, error) {
	return nil, errors.New("lookup unavailable")
}

func TestAHPShadowLookupFailureMarksBothPostures(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.riskScanner = &ahpFailingShadowScanner{}
	event := ahpTestEvent("tool.before")
	p, _, err := normalizeAHPEvent(event, auth, "intercept")
	require.NoError(t, err)
	for _, open := range []bool{false, true} {
		ti.service.productFeatures = staticFeatures{failOpen: open}
		attrs := map[attr.Key]any{}
		tracked, failure := withAHPFailureTracker(context.WithValue(ctx, authenticatedIngestOptionsKey{}, AuthenticatedIngestOptions{AHPPolicy: true, SourceAttributes: attrs}))
		reason, _ := ti.service.evaluateCanonicalShadowMCP(tracked, auth, canonicalActor{UserID: auth.UserID}, p, "read_file", map[string]any{})
		if open {
			require.Empty(t, reason)
		} else {
			require.NotEmpty(t, reason)
		}
		require.Equal(t, "mcp_policy_evaluation_unavailable", *failure)
		require.Equal(t, *failure, attrs[attr.Key("gram.hook.enforcement_gap")])
	}
}

func TestCanonicalProvenanceTrimsAcceptedType(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	p := &gen.IngestPayload{SchemaVersion: hookIngestSchemaV1, Source: &gen.HookIngestSource{Adapter: "example"}, Event: &gen.HookIngestEvent{Type: "  tool.completed \t"}}
	require.NoError(t, validateCanonicalIngestPayload(p))
	attrs := hookTelemetryBaseAttrs(p, &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project}, telemetryHookEventName(p), "example")
	require.Equal(t, "PostToolUse", telemetryHookEventName(p))
	require.Equal(t, "tool.completed", attrs[attr.Key("gram.hook.canonical_event")])
}

// Invalid notifications remain silent HTTP 204, but never reach callbacks.
func TestAHPActualSDKRejectsInvalidUsageAndOutcome(t *testing.T) {
	t.Parallel()
	calls := 0
	handler, err := ahpserver.NewHandler(ahpserver.Handlers{Observe: func(context.Context, ahp.ObserveNotification) error { calls++; return nil }}, ahpserver.Options{})
	require.NoError(t, err)
	model := func() map[string]any {
		return map[string]any{"id": "example", "source": "urn:example:harness", "time": "2026-09-15T12:00:00Z", "type": "model.response.after", "model": map[string]any{"id": "example", "provider": "example"}, "attempt": map[string]any{"id": "attempt-1", "number": 1}, "execution": map[string]any{"status": "executed"}, "items": []any{}, "finishReason": "stop", "usage": map[string]any{"kind": "amount", "scope": "attempt", "completeness": "complete", "provenance": "provider", "inputTokens": 12}}
	}
	request := func(e map[string]any) *httptest.ResponseRecorder {
		b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "hooks/observe", "params": map[string]any{"protocolVersion": "draft", "event": e}})
		require.NoError(t, err)
		r := httptest.NewRequest("POST", "/ahp", bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, http.StatusNoContent, request(model()).Code)
	require.Equal(t, 1, calls)
	for _, eventType := range []string{"model.response.after", "model.error"} {
		for _, mutation := range []string{"negative_input", "negative_output", "negative_cache_read", "negative_cache_write", "turn_scope"} {
			e := model()
			e["type"] = eventType
			if eventType == "model.error" {
				delete(e, "finishReason")
				e["error"] = map[string]any{"class": "operation", "message": "failed"}
			}
			u, ok := e["usage"].(map[string]any)
			require.True(t, ok)
			switch mutation {
			case "negative_input":
				u["inputTokens"] = -1
			case "negative_output":
				u["outputTokens"] = -1
			case "negative_cache_read":
				u["cacheReadTokens"] = -1
			case "negative_cache_write":
				u["cacheWriteTokens"] = -1
			case "turn_scope":
				u["scope"] = "turn"
				u["kind"] = "total"
			}
			require.Equal(t, http.StatusNoContent, request(e).Code, eventType+" "+mutation)
			require.Equal(t, 1, calls)
			if mutation == "turn_scope" {
				project := uuid.New()
				p, attrs, err := normalizeAHPEvent(e, &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project}, "observe")
				require.NoError(t, err)
				require.Nil(t, p.Data.Usage)
				require.NotContains(t, attrs, attr.Key("gram.hook.usage_authority"))
			}
		}
	}
	for _, outcome := range []string{"", "unrecognized"} {
		e := ahpTestEvent("tool.after")
		e["outcome"] = outcome
		e["execution"] = map[string]any{"status": "executed"}
		e["items"] = []any{}
		require.Equal(t, http.StatusNoContent, request(e).Code)
		delete(e, "outcome")
		require.Equal(t, http.StatusNoContent, request(e).Code)
		require.Equal(t, 1, calls)
	}
}

func TestAHPOptionalMetadataDoesNotReplaceToolEvidence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = fixedHookAuthorizer{authCtx: auth}
	ti.service.productFeatures = staticFeatures{failOpen: false}
	scanner := &ahpContentScanner{}
	ti.service.riskScanner = scanner
	handler, err := ti.service.ahpHandler()
	require.NoError(t, err)
	metadata := map[string]any{"id": "item-1", "kind": "message", "mediaType": "text/plain", "role": "user", "selection": "metadata"}
	event := ahpTestEvent("tool.before")
	event["items"] = []any{metadata}
	request := func(event map[string]any) ahp.InterceptResponseResult {
		b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": event["id"], "method": "hooks/intercept", "params": map[string]any{"protocolVersion": "draft", "event": event, "capabilities": map[string]any{"effects": []string{"deny"}}}})
		require.NoError(t, err)
		r := httptest.NewRequest("POST", "/ahp", bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		parsed := ahp.ParseInterceptResponse(w.Body.Bytes())
		require.True(t, parsed.OK, w.Body.String())
		return parsed.Value.Result
	}
	_, gaps := ti.service.resolveAHPContent(ctx, event)
	require.True(t, gaps, "the optional metadata gap must remain observable")
	require.Empty(t, request(event).Effects)
	require.Equal(t, []string{`{"path":"example.txt"}`}, scanner.texts)
	scanner.texts = nil
	prompt := map[string]any{"id": "prompt-1", "source": event["source"], "time": event["time"], "type": "turn.start", "session": event["session"], "turn": map[string]any{"id": "turn-1"}, "trigger": "user", "items": []any{metadata}}
	require.Len(t, request(prompt).Effects, 1)
	require.Empty(t, scanner.texts, "missing prompt bytes must not be scanned as complete")
	data := []byte("partial user prompt")
	ref := ahp.ContentReference{Ref: "ahp-content:" + uuid.NewString(), Size: json.Number(fmt.Sprint(len(data))), Sha256: fmt.Sprintf("%x", sha256.Sum256(data))}
	scope, err := ahpContentScope(ctx)
	require.NoError(t, err)
	require.NoError(t, ti.service.cache.Set(ctx, "ahp:content:v1:"+scope+":"+ref.Ref, ahpCachedContent{Reference: ref, Bytes: data}, ahpContentTTL))
	prompt["id"] = "prompt-2"
	prompt["items"] = []any{map[string]any{"id": "item-2", "kind": "message", "mediaType": "text/plain", "role": "user", "selection": "body", "body": ref}, metadata}
	require.Len(t, request(prompt).Effects, 1)
	require.Empty(t, scanner.texts, "partial prompt coverage still follows failure posture")
	tool, ok := event["tool"].(map[string]any)
	require.True(t, ok)
	delete(tool, "input")
	result, err := ti.service.ingestAHP(ctx, event, "intercept", true)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision, "trusted seam must not fabricate absent input evidence")
}

type ahpCountingContentCache struct {
	cache.Cache
	reads int
}

func (c *ahpCountingContentCache) Get(ctx context.Context, key string, value any) error {
	if strings.HasPrefix(key, "ahp:content:v1:") {
		c.reads++
	}
	if err := c.Cache.Get(ctx, key, value); err != nil {
		return fmt.Errorf("read test content: %w", err)
	}
	return nil
}

func TestAHPContentReadsAreMemoizedAndBounded(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	counter := &ahpCountingContentCache{Cache: ti.service.cache}
	ti.service.cache = counter
	scope, err := ahpContentScope(ctx)
	require.NoError(t, err)
	item := func(data []byte, id string) map[string]any {
		ref := ahp.ContentReference{Ref: "ahp-content:" + uuid.NewString(), Size: json.Number(fmt.Sprint(len(data))), Sha256: fmt.Sprintf("%x", sha256.Sum256(data))}
		require.NoError(t, counter.Set(ctx, "ahp:content:v1:"+scope+":"+ref.Ref, ahpCachedContent{Reference: ref, Bytes: data}, ahpContentTTL))
		return map[string]any{"id": id, "kind": "message", "mediaType": "text/plain", "role": "user", "selection": "body", "body": ref}
	}
	repeated := item([]byte("example"), "item-1")
	r, gap := ti.service.resolveAHPContent(ctx, map[string]any{"items": []any{repeated, repeated, repeated}})
	require.False(t, gap)
	require.Len(t, r, 1)
	require.Equal(t, 1, counter.reads)
	oversized := item(bytes.Repeat([]byte("a"), ahpMaxContentBytes), "large")
	late := item([]byte("never read after byte cap"), "late")
	counter.reads = 0
	r, gap = ti.service.resolveAHPContent(ctx, map[string]any{"items": []any{oversized, oversized, oversized, oversized, oversized, late}})
	require.True(t, gap)
	require.Empty(t, r)
	require.Equal(t, 1, counter.reads, "repeated bytes count against the cap but require only one read; traversal stops at overflow")
	descriptors := []any{}
	for i := range ahpMaxContentReferences + 2 {
		descriptors = append(descriptors, item([]byte("x"), fmt.Sprint(i)))
	}
	counter.reads = 0
	r, gap = ti.service.resolveAHPContent(ctx, map[string]any{"items": descriptors})
	require.True(t, gap)
	require.Empty(t, r)
	require.Equal(t, ahpMaxContentReferences, counter.reads, "no reads after distinct-reference admission limit")
}
