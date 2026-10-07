package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ahp "github.com/agenthooksprotocol/go-sdk"
	ahpauth "github.com/agenthooksprotocol/go-sdk/auth"
	ahpclient "github.com/agenthooksprotocol/go-sdk/client"
	"github.com/google/uuid"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type ahpSDKAuth struct{}

func (ahpSDKAuth) Credential(context.Context, ahpauth.Request) (ahpauth.Credential, error) {
	return ahpauth.Credential{Type: "bearer", Token: "example"}, nil
}
func (ahpSDKAuth) Challenge(context.Context, ahpauth.Request, ahpauth.Challenge) (*ahpauth.Credential, error) {
	return nil, nil
}

type ahpContentScanner struct {
	stubResultScanner
	texts []string
}

func (s *ahpContentScanner) ScanForEnforcement(_ context.Context, r risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	s.texts = append(s.texts, r.Text)
	return s.result, nil
}

func TestAHPRealSDKUploadsInterceptAndObserveContent(t *testing.T) {
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = fixedHookAuthorizer{authCtx: auth}
	ti.service.productFeatures = alwaysEnabledFeatures{}
	scanner := &ahpContentScanner{}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle("/ahp", h)
	mux.Handle("/ahp/content", ti.service.ahpAuthenticate(http.HandlerFunc(ti.service.ahpUploadContent)))
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, r)
		if recorder.Code != 200 && recorder.Code != 201 && recorder.Code != 204 {
			t.Logf("test receiver %s status=%d", r.URL.Path, recorder.Code)
		}
		maps.Copy(w.Header(), recorder.Header())
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer peer.Close()
	subscriptions := []any{}
	for _, v := range []struct{ event, mode string }{{"turn.start", "intercept"}, {"user.message.outbound", "observe"}, {"tool.after", "observe"}} {
		sub := map[string]any{"events": []string{v.event}, "mode": v.mode, "scope": "project", "timeoutMs": 5000, "content": map[string]any{"default": "body"}, "upload": map[string]any{"endpoint": peer.URL + "/ahp/content", "timeoutMs": 5000, "maxBytes": ahpMaxContentBytes, "auth": map[string]any{"type": "bearer", "tokenRef": "example"}}}
		if v.mode == "intercept" {
			sub["failurePolicy"] = "fail-closed"
		} else {
			delete(sub, "timeoutMs")
		}
		subscriptions = append(subscriptions, sub)
	}
	b, err := json.Marshal(map[string]any{"protocolVersion": "draft", "hooks": []any{map[string]any{"id": "com.example.gram", "transport": map[string]any{"type": "http", "url": peer.URL + "/ahp"}, "authentication": map[string]any{"type": "bearer", "tokenRef": "example"}, "subscriptions": subscriptions}}})
	require.NoError(t, err)
	reg := ahp.ParseRegistration(b)
	require.True(t, reg.OK, reg.Diagnostics)
	cap, err := ahpCapabilities()
	require.NoError(t, err)
	b, err = json.Marshal(cap.Manifest)
	require.NoError(t, err)
	var manifest ahp.StaticCapabilityManifest
	require.NoError(t, json.Unmarshal(b, &manifest))
	host, err := ahpclient.New(reg.Value, ahpclient.Options{Source: "urn:example:custom-harness", Manifest: manifest, AuthProvider: ahpSDKAuth{}, Content: ahpclient.ContentOptions{AllowLoopbackHTTP: true, AuthorizeContent: func(context.Context, ahpclient.ContentAuthorization) (bool, error) { return true, nil }, ProjectOpaque: func(_ context.Context, _ ahpclient.ContentAuthorization, _ string, value any) (any, error) {
		return value, nil
	}}})
	require.NoError(t, err)
	defer func() { require.NoError(t, host.Close()) }()
	session := uuid.NewString()
	for _, v := range []struct{ name, text, path string }{{"turn.start", "A real user prompt", "/items/0"}, {"user.message.outbound", "An actual assistant answer", "/message/payload/0"}, {"tool.after", "Tool result body", "/items/0"}} {
		item := map[string]any{"id": uuid.NewString(), "kind": "message", "mediaType": "text/plain", "selection": "metadata", "role": "user"}
		input := map[string]any{"session": map[string]any{"id": session}, "turn": map[string]any{"id": "turn-1"}, "items": []any{item}}
		switch v.name {
		case "turn.start":
			input["trigger"] = "user"
		case "user.message.outbound":
			delete(input, "items")
			input["message"] = map[string]any{"channel": "chat", "payload": []any{item}}
		case "tool.after":
			input["tool"] = map[string]any{"name": "read_file", "input": map[string]any{}, "origin": "native"}
			input["call"] = map[string]any{"id": "call-1"}
			input["path"] = "example"
			input["outcome"] = "ok"
			input["execution"] = map[string]any{"status": "executed"}
		}
		result, err := host.Dispatch(ctx, v.name, input, ahpclient.WithContentSource(v.path, ahpclient.NewContentSource(io.NopCloser(strings.NewReader(v.text)))))
		require.NoError(t, err, v.name)
		require.Empty(t, result.Errors)
		require.Empty(t, result.Diagnostics)
		require.False(t, result.Interrupted)
	}
	require.Equal(t, []string{"A real user prompt"}, scanner.texts)
	scoped := ahpIdentity("ahp-session", auth.ActiveOrganizationID, auth.ProjectID.String(), ahpPrincipalIdentity(ctx, auth), "urn:example:custom-harness", session)
	messages, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: sessionIDToUUID(scoped), ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
}

func TestAHPContentFramingAndScope(t *testing.T) {
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	raw := []byte("private textual bytes")
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	upload := func(body []byte, size int64, digest string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/ahp/content", bytes.NewReader(body)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set("Content-Length", fmt.Sprint(size))
		r.ContentLength = size
		r.Header.Set("AHP-Content-SHA256", digest)
		w := httptest.NewRecorder()
		ti.service.ahpUploadContent(w, r)
		return w
	}
	valid := upload(raw, int64(len(raw)), sum)
	require.Equal(t, 201, valid.Code, valid.Body.String())
	ref := ahp.ParseContentReference(valid.Body.Bytes())
	require.True(t, ref.OK)
	item := map[string]any{"id": "item-1", "kind": "message", "mediaType": "text/plain", "selection": "body", "body": ref.Value}
	event := map[string]any{"items": []any{item}}
	resolved, gap := ti.service.resolveAHPContent(ctx, event)
	require.False(t, gap)
	require.Equal(t, string(raw), resolved[ref.Value.Ref])
	for _, part := range []string{"org", "project", "principal"} {
		copyAuth := *auth
		switch part {
		case "org":
			copyAuth.ActiveOrganizationID = "org_other"
		case "project":
			project := uuid.New()
			copyAuth.ProjectID = &project
		case "principal":
			copyAuth.UserID = "user_other"
		}
		other := contextvalues.SetAuthContext(context.Background(), &copyAuth)
		if part == "principal" {
			other = contextvalues.WithAuthenticatedActor(context.Background(), &copyAuth, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))
		}
		r, gap := ti.service.resolveAHPContent(other, event)
		require.True(t, gap)
		require.Empty(t, r)
	}
	require.Equal(t, 400, upload(raw, int64(len(raw)), strings.Repeat("0", 64)).Code)
	require.Equal(t, 400, upload(raw, int64(len(raw)+1), sum).Code)
	require.Equal(t, 413, upload(raw, ahpMaxContentBytes+1, sum).Code)
	require.Equal(t, 400, upload([]byte{255}, 1, fmt.Sprintf("%x", sha256.Sum256([]byte{255}))).Code)
	// Caller-selected external refs are rejected locally, never fetched.
	item["body"] = map[string]any{"ref": "http://127.0.0.1/private", "size": len(raw), "sha256": sum}
	resolved, gap = ti.service.resolveAHPContent(ctx, event)
	require.True(t, gap)
	require.Empty(t, resolved)
}

func TestAHPContentOccurrenceBudgetAndOuterDescriptor(t *testing.T) {
	ctx, ti := newTestHooksService(t)
	raw := bytes.Repeat([]byte("a"), ahpMaxContentBytes)
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	ref := ahp.ContentReference{Ref: "ahp-content:" + uuid.NewString(), Size: json.Number(fmt.Sprint(len(raw))), Sha256: sum}
	scope, err := ahpContentScope(ctx)
	require.NoError(t, err)
	require.NoError(t, ti.service.cache.Set(ctx, "ahp:content:v1:"+scope+":"+ref.Ref, ahpCachedContent{Reference: ref, Bytes: raw}, ahpContentTTL))
	item := map[string]any{"id": "item-1", "kind": "message", "mediaType": "text/plain", "selection": "body", "role": "assistant", "body": ref, "size": float64(len(raw)), "sha256": sum}
	event := map[string]any{"items": []any{item}}
	r, gap := ti.service.resolveAHPContent(ctx, event)
	require.False(t, gap)
	require.Len(t, r, 1)
	item["size"] = float64(1)
	r, gap = ti.service.resolveAHPContent(ctx, event)
	require.True(t, gap)
	require.Empty(t, r)
	item["size"] = float64(len(raw))
	item["sha256"] = strings.Repeat("0", 64)
	r, gap = ti.service.resolveAHPContent(ctx, event)
	require.True(t, gap)
	require.Empty(t, r)
	item["sha256"] = sum
	items := []any{}
	for i := range 5 {
		copy := map[string]any{}
		maps.Copy(copy, item)
		copy["id"] = fmt.Sprint(i)
		items = append(items, copy)
	}
	event["items"] = items
	r, gap = ti.service.resolveAHPContent(ctx, event)
	require.True(t, gap)
	require.Empty(t, r, "over-budget materialized content must not be partially accepted")
}

func TestAHPContentIgnoresOpaqueSelections(t *testing.T) {
	ctx, ti := newTestHooksService(t)
	event := ahpTestEvent("tool.before")
	event["tool"].(map[string]any)["input"] = map[string]any{"selection": "all", "nested": map[string]any{"id": "not-an-item", "selection": "body", "mediaType": "text/plain", "body": map[string]any{"ref": "https://never-fetch.example/private", "size": 1, "sha256": strings.Repeat("0", 64)}}}
	wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": event["id"], "method": "hooks/intercept", "params": map[string]any{"protocolVersion": "draft", "event": event, "capabilities": map[string]any{"effects": []string{"deny"}}}})
	require.NoError(t, err)
	parsed := ahp.ParseInterceptRequest(wire)
	require.True(t, parsed.OK, parsed.Diagnostics)
	r, gap := ti.service.resolveAHPContent(ctx, parsed.Value.Params.Event)
	require.False(t, gap)
	require.Empty(t, r)
}
