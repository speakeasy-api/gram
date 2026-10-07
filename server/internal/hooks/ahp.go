package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	ahp "github.com/agenthooksprotocol/go-sdk"
	ahpserver "github.com/agenthooksprotocol/go-sdk/server"
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpidentity "github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	goahttp "goa.design/goa/v3/http"
)

// AttachAHP adds the authenticated AHP transport without changing legacy routes.
func AttachAHP(mux goahttp.Muxer, s *Service) error {
	h, err := s.ahpHandler()
	if err != nil {
		return err
	}
	mux.Handle("POST", "/ahp", h.ServeHTTP)
	mux.Handle("POST", "/ahp/content", s.ahpAuthenticate(http.HandlerFunc(s.ahpUploadContent)).ServeHTTP)
	return nil
}

func (s *Service) ahpHandler() (http.Handler, error) {
	h, err := ahpserver.NewHandler(ahpserver.Handlers{
		Intercept: func(ctx context.Context, req ahp.InterceptRequest) (ahp.InterceptResponseResult, error) {
			canDeny := ahpSupportsDeny(req.Params.Capabilities)
			result, err := s.ingestAHP(ctx, req.Params.Event, "intercept", canDeny)
			response := ahp.InterceptResponseResult{ProtocolVersion: ptrAHP(ahp.ProtocolVersion("draft")), Effects: []*ahp.Effect{}, Extensions: ahp.Optional[*ahp.Extensions]{Value: nil, Present: false}, AdditionalProperties: nil}
			reason := ""
			if err != nil {
				if !s.ahpFailOpen(ctx) && canDeny {
					reason = "Hook policy evaluation unavailable"
				}
			} else if result.Decision == "deny" && canDeny {
				reason = conv.PtrValOr(result.Message, "Hook policy denied this operation")
			}
			if reason != "" {
				var effect ahp.Effect
				b, _ := json.Marshal(map[string]any{"type": "deny", "reason": reason})
				if err := json.Unmarshal(b, &effect); err != nil {
					return response, fmt.Errorf("decode AHP deny effect: %w", err)
				}
				response.Effects = append(response.Effects, &effect)
			}
			return response, nil
		},
		Observe: func(ctx context.Context, req ahp.ObserveNotification) error {
			_, err := s.ingestAHP(ctx, req.Params.Event, "observe", false)
			return err
		},
		Capabilities: func(context.Context, ahp.CapabilitiesRequest) (ahp.CapabilitiesResponseResult, error) {
			return ahpCapabilities()
		},
	}, ahpserver.Options{MaxRequestBytes: 1 << 20, MaxResponseBytes: 64 << 10})
	if err != nil {
		return nil, fmt.Errorf("create AHP handler: %w", err)
	}
	return s.ahpAuthenticate(h), nil
}

func (s *Service) ahpAuthenticate(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Gram-Key"))
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			fields := strings.Fields(authorization)
			if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || (key != "" && key != fields[1]) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			key = fields[1]
		}
		if key == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		project := strings.TrimSpace(r.Header.Get("Gram-Project"))
		ctx, err := s.authorizePluginRequest(r.Context(), key, project)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, errAgentHooksDenied) {
				status = http.StatusForbidden
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil || authCtx.ProjectID == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ahpSupportsDeny(capabilities any) bool {
	b, _ := json.Marshal(capabilities)
	var c struct {
		Effects []json.RawMessage `json:"effects"`
	}
	_ = json.Unmarshal(b, &c)
	for _, e := range c.Effects {
		if string(e) == `"deny"` {
			return true
		}
	}
	return false
}

func (s *Service) ingestAHP(ctx context.Context, event any, mode string, canDeny bool) (*gen.IngestHookResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, fmt.Errorf("AHP authentication required")
	}
	resolved, gaps := s.resolveAHPContent(ctx, event)
	payload, attrs, err := normalizeAHPEventWithContent(event, authCtx, mode, resolved, ahpPrincipalIdentity(ctx, authCtx))
	if err != nil {
		return nil, err
	}
	if payload.Session != nil && s.cache != nil {
		key := "ahp:harness:v1:" + ahpIdentity(authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), *payload.Session.ID)
		var harness gen.HookIngestSource
		if payload.Event.Type == "session.started" && payload.Source.AdapterVersion != nil {
			_ = s.cache.Set(ctx, key, *payload.Source, sessionMetadataTTL)
		} else if s.cache.Get(ctx, key, &harness) == nil && harness.Adapter != "" {
			payload.Source.Adapter = harness.Adapter
			payload.Source.AdapterVersion = harness.AdapterVersion
			_ = s.cache.Expire(ctx, key, sessionMetadataTTL)
		}
	}
	if gaps {
		attrs[attr.Key("gram.hook.evidence_gap")] = "content_reference_unavailable_or_unsupported"
	}
	if mode == "intercept" && !canDeny {
		attrs[attr.Key("gram.hook.enforcement_gap")] = "deny_not_supported"
	}
	return s.IngestAuthenticatedWithOptions(ctx, authCtx, payload, AuthenticatedIngestOptions{
		// Tool enforcement uses its explicit input, not optional content views.
		EvidenceUnavailable: gaps && payload.Event.Type == "prompt.submitted",
		ObserveOnly:         mode == "observe" || !canDeny,
		AHPPolicy:           true, CapabilitySpendGate: canDeny,
		AllowWarnAcknowledgement: false, AllowSessionIdentityFallback: false,
		SourceAttributes: attrs, OutputToolCalls: nil, OriginatingClient: "",
	})
}

func (s *Service) ahpFailOpen(ctx context.Context) bool {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || s.productFeatures == nil {
		return false
	}
	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureHooksFailOpen)
	return err == nil && enabled
}

func ahpIdentity(parts ...string) string {
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Only semantic fields are translated. Native/extensions and arbitrary payloads
// are never copied into Raw or generic telemetry (which is metadata-only).
func normalizeAHPEvent(event any, authCtx *contextvalues.AuthContext, mode string) (*gen.IngestPayload, map[attr.Key]any, error) {
	return normalizeAHPEventWithContent(event, authCtx, mode, nil, authCtx.UserID)
}

func normalizeAHPEventWithContent(event any, authCtx *contextvalues.AuthContext, mode string, resolved map[string]string, principal string) (*gen.IngestPayload, map[attr.Key]any, error) {
	b, err := json.Marshal(event)
	if err != nil {
		return nil, nil, fmt.Errorf("encode AHP event: %w", err)
	}
	var e struct {
		ID      string `json:"id"`
		Source  string `json:"source"`
		Type    string `json:"type"`
		Time    string `json:"time"`
		Session struct {
			ID    string          `json:"id"`
			Cwd   *string         `json:"cwd"`
			Model json.RawMessage `json:"model"`
		} `json:"session"`
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
		Call struct {
			ID string `json:"id"`
		} `json:"call"`
		Tool struct {
			Name  string `json:"name"`
			Input any    `json:"input"`
			MCP   *struct {
				Server struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"server"`
				ToolName   string `json:"toolName"`
				Provenance string `json:"provenance"`
				Connection struct {
					URL     string            `json:"url"`
					Command string            `json:"command"`
					Args    []string          `json:"args"`
					Gaps    []json.RawMessage `json:"gaps"`
				} `json:"connection"`
			} `json:"mcp"`
		} `json:"tool"`
		Outcome   string `json:"outcome"`
		Execution struct {
			Status string `json:"status"`
		} `json:"execution"`
		Items    []json.RawMessage `json:"items"`
		Error    any               `json:"error"`
		Duration *float64          `json:"durationMs"`
		Message  struct {
			Items   []json.RawMessage `json:"text"`
			Payload []json.RawMessage `json:"payload"`
			Sender  string            `json:"sender"`
			Channel string            `json:"channel"`
		} `json:"message"`
		Harness struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"harness"`
		Trigger string `json:"trigger"`
		Current struct {
			ID string `json:"id"`
		} `json:"current"`
		Model struct {
			ID string `json:"id"`
		} `json:"model"`
		Usage *struct {
			Kind         string `json:"kind"`
			Scope        string `json:"scope"`
			Completeness string `json:"completeness"`
			Provenance   string `json:"provenance"`
			Input        *int   `json:"inputTokens"`
			Output       *int   `json:"outputTokens"`
			CacheRead    *int   `json:"cacheReadTokens"`
			CacheWrite   *int   `json:"cacheWriteTokens"`
			Cost         *struct {
				Amount   float64 `json:"amount"`
				Currency string  `json:"currency"`
				Basis    string  `json:"basis"`
			} `json:"cost"`
		} `json:"usage"`
	}
	if err = json.Unmarshal(b, &e); err != nil {
		return nil, nil, fmt.Errorf("decode AHP event: %w", err)
	}
	if authCtx.ProjectID == nil || *authCtx.ProjectID == uuid.Nil {
		return nil, nil, fmt.Errorf("AHP project required")
	}
	// URI is an opaque source identity, not a credential, user or trusted host.
	// Hash for display to avoid leaking URI userinfo/query strings and bound labels.
	source := ahpSourceLabel(e.Source)
	session := ahpIdentity("ahp-session", authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), principal, e.Source, e.Session.ID)
	if e.Session.ID == "" {
		session = ""
	}
	id := ahpIdentity("ahp-event", authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), principal, e.Source, e.ID)
	p := &gen.IngestPayload{ApikeyToken: nil, ProjectSlugInput: nil, Replayed: nil, Raw: nil, SchemaVersion: hookIngestSchemaV1, IdempotencyKey: &id, Source: &gen.HookIngestSource{Adapter: source, RawEventName: &e.Type, AdapterVersion: nil, Hostname: nil, UserEmail: nil}, Session: &gen.HookIngestSession{ID: &session, Cwd: e.Session.Cwd, TurnID: nil, Model: nil}, Event: &gen.HookIngestEvent{Type: "ahp." + e.Type, OccurredAt: &e.Time}, Data: &gen.HookIngestData{Prompt: nil, ToolCall: nil, Mcp: nil, McpInventory: nil, McpInventoryCollected: nil, Usage: nil, Message: nil, Skill: nil, Notification: nil, McpAttribution: nil, PromptAttachments: nil}}
	if session == "" {
		p.Session = nil
	}
	if e.Turn.ID != "" && p.Session != nil {
		p.Session.TurnID = new(ahpIdentity("ahp-turn", authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), principal, session, e.Source, e.Turn.ID))
	}
	if p.Session != nil {
		var model string
		if json.Unmarshal(e.Session.Model, &model) == nil && model != "" {
			p.Session.Model = &model
		}
	}
	if e.Model.ID != "" && p.Session != nil {
		p.Session.Model = &e.Model.ID
	}
	attrs := map[attr.Key]any{attr.Key("gram.hook.transport"): "ahp", attr.Key("gram.hook.mode"): mode, attr.Key("gram.hook.ahp_event"): e.Type, attr.Key("gram.hook.source_id"): ahpIdentity(e.Source)}
	if p.Session == nil {
		attrs[attr.Key("gram.hook.session_gap")] = "session_not_reported"
	}
	if e.Message.Sender != "" {
		attrs[attr.Key("gram.hook.message_sender")] = boundedAHPLabel(e.Message.Sender)
	}
	if e.Message.Channel != "" {
		attrs[attr.Key("gram.hook.message_channel")] = boundedAHPLabel(e.Message.Channel)
	}
	if e.Harness.Name != "" {
		p.Source.Adapter = "ahp-harness:" + boundedAHPLabel(e.Harness.Name)
		p.Source.AdapterVersion = new(boundedAHPLabel(e.Harness.Version))
	}
	if e.Outcome != "" {
		attrs[attr.Key("gram.hook.outcome")] = e.Outcome
	}
	if e.Execution.Status != "" {
		attrs[attr.Key("gram.hook.execution_status")] = e.Execution.Status
	}
	switch e.Type {
	case "session.start":
		p.Event.Type = "session.started"
	case "session.end":
		p.Event.Type = "session.ended"
	case "turn.start":
		if e.Trigger == "user" {
			p.Event.Type = "prompt.submitted"
			if text := ahpText(e.Items, resolved); text != "" || ahpHasResolvedText(e.Items, resolved) {
				p.Data.Prompt = &gen.HookPromptData{Text: &text}
			} else {
				attrs[attr.Key("gram.hook.evidence_gap")] = "prompt_content_unavailable"
			}
		}
	case "user.message.inbound":
		p.Event.Type = "ahp.user.message.inbound"
		if text := ahpText(e.Message.Items, resolved); text != "" || ahpHasResolvedText(e.Message.Items, resolved) {
			p.Data.Message = &gen.HookMessageData{Text: &text, Role: new("external"), DurationMs: nil}
		} else {
			attrs[attr.Key("gram.hook.evidence_gap")] = "external_message_content_unavailable"
		}
	case "user.message.outbound":
		p.Event.Type = "assistant.responded"
		if text := ahpText(e.Message.Payload, resolved); text != "" || ahpHasResolvedText(e.Message.Payload, resolved) {
			p.Data.Message = &gen.HookMessageData{Text: &text, Role: nil, DurationMs: nil}
		} else {
			attrs[attr.Key("gram.hook.evidence_gap")] = "message_content_unavailable"
		}
	case "tool.before", "tool.permission.request":
		p.Event.Type = "tool.requested"
	case "tool.after":
		if e.Execution.Status == "executed" && e.Outcome != "denied" {
			if e.Outcome == "ok" {
				p.Event.Type = "tool.completed"
			} else {
				p.Event.Type = "tool.failed"
			}
		} else {
			p.Event.Type = "tool.skipped"
		}
	case "model.response.after", "model.error":
		p.Event.Type = "usage.reported"
	case "model.switch.after", "config.change.after":
		p.Event.Type = "session.updated"
		if e.Current.ID != "" && p.Session != nil {
			p.Session.Model = &e.Current.ID
		}
	case "user.attention":
		p.Event.Type = "notification.reported"
	}
	if e.Tool.Name != "" {
		if e.Tool.Input == nil {
			attrs[attr.Key("gram.hook.evidence_gap")] = "tool_input_unavailable"
		}
		if e.Type == "tool.after" && len(e.Items) > 0 && !ahpHasResolvedText(e.Items, resolved) {
			attrs[attr.Key("gram.hook.evidence_gap")] = "tool_output_content_unavailable"
		}
		callID := ""
		if e.Call.ID != "" {
			callID = ahpIdentity("ahp-call", authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), principal, session, e.Source, e.Call.ID)
		}
		p.Data.ToolCall = &gen.HookToolCallData{ID: &callID, Name: &e.Tool.Name, Input: e.Tool.Input, Error: sanitizeAHPError(e.Error), DurationMs: e.Duration, Status: new(e.Outcome), Output: nil, IsInterrupt: nil, PermissionType: nil}
		if e.Type == "tool.after" && e.Execution.Status == "executed" && e.Outcome != "denied" {
			if text := ahpText(e.Items, resolved); text != "" || ahpHasResolvedText(e.Items, resolved) {
				p.Data.ToolCall.Output = text
			}
		}
		if e.Type == "tool.permission.request" {
			p.Data.ToolCall.PermissionType = new("permission")
		}
		if p.Event.Type != "tool.requested" && p.Event.Type != "tool.completed" && p.Event.Type != "tool.failed" && p.Event.Type != "tool.skipped" {
			p.Data.ToolCall.Input = nil
			p.Data.ToolCall.Error = nil
		}
		if m := e.Tool.MCP; m != nil {
			p.Data.Mcp = &gen.HookMCPData{URL: nil, Command: nil, ResultJSON: nil, ServerIdentity: new(m.Server.ID), ServerName: new(m.Server.Name)}
			if m.Connection.URL != "" {
				if safe, ok := mcpidentity.RedactServerURL(m.Connection.URL); ok {
					p.Data.Mcp.URL = &safe
				} else {
					attrs[attr.Key("gram.hook.evidence_gap")] = "mcp_url_unavailable"
				}
			}
			if m.Connection.Command != "" {
				parts := []string{m.Connection.Command}
				parts = append(parts, m.Connection.Args...)
				command := mcpidentity.RedactCommand(strings.Join(parts, " "))
				p.Data.Mcp.Command = &command
			}
			attrs[attr.Key("gram.hook.mcp_provenance")] = boundedAHPLabel(m.Provenance)
			if len(m.Connection.Gaps) > 0 {
				attrs[attr.Key("gram.hook.evidence_gap")] = "mcp_connection_fields_unavailable"
			}
		}
	}
	// Attempt usage belongs only to model response/error. Outbound user messages
	// contain presentation content, not a second copy of token usage. Turn totals
	// are not emitted as usage (they would double-count model attempt deltas).
	if e.Usage != nil && e.Usage.Scope == "attempt" && e.Usage.Kind == "amount" && (e.Type == "model.response.after" || e.Type == "model.error") {
		attrs[attr.Key("gram.hook.usage_authority")] = "model_attempt"
		u := e.Usage
		attrs[attr.Key("gram.hook.usage_scope")] = u.Scope
		attrs[attr.Key("gram.hook.usage_completeness")] = u.Completeness
		attrs[attr.Key("gram.hook.usage_provenance")] = u.Provenance
		attrs[attr.Key("gram.hook.usage_kind")] = u.Kind
		p.Data.Usage = &gen.HookUsageData{Cost: nil, LoopCount: nil, Status: nil, InputTokens: u.Input, OutputTokens: u.Output, CacheReadTokens: u.CacheRead, CacheWriteTokens: u.CacheWrite}
		if u.Cost != nil {
			attrs[attr.Key("gram.hook.cost_currency")] = u.Cost.Currency
			attrs[attr.Key("gram.hook.cost_basis")] = u.Cost.Basis
			if u.Cost.Currency == "USD" {
				p.Data.Usage.Cost = &u.Cost.Amount
			}
		}
	}
	return p, attrs, nil
}

func ahpText(items []json.RawMessage, resolved map[string]string) string {
	var texts []string
	for _, raw := range items {
		var item struct {
			Selection string `json:"selection"`
			Body      struct {
				Ref string `json:"ref"`
			} `json:"body"`
		}
		if json.Unmarshal(raw, &item) == nil && item.Selection == "body" {
			if text, ok := resolved[item.Body.Ref]; ok {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func ahpCapabilities() (ahp.CapabilitiesResponseResult, error) {
	events := []any{}
	for event := range strings.FieldsSeq("tool.before tool.after session.start session.end config.change.before config.change.after turn.start turn.finish.before turn.end turn.progress model.request.before model.response.after model.error model.switch.before model.switch.after tool.permission.request tool.permission.resolved tool.progress tool.batch.after context.compact.before context.compact.after task.change.before task.change.after user.attention user.elicitation.request user.elicitation.result user.message.inbound user.message.outbound workspace.change.before workspace.change.after file.changed hook.failure") {
		entry := map[string]any{"event": event, "modes": []string{"observe"}}
		if event == "tool.before" || event == "tool.permission.request" || event == "turn.start" {
			entry["modes"] = []string{"observe", "intercept"}
			entry["capabilities"] = map[string]any{"effects": []string{"deny"}}
		}
		events = append(events, entry)
	}
	b, err := json.Marshal(map[string]any{"protocolVersion": "draft", "manifest": map[string]any{
		"events": events, "gaps": []any{map[string]string{"path": "effects", "reason": "Only deny is supported; no human acknowledgement, modify, inject or flow effects"}, map[string]string{"path": "observe", "reason": "Evidence capture only; realtime enforcement evaluation is not run"}, map[string]string{"path": "events", "reason": "Unmapped events retain metadata only; no native payload or extensions are stored"}, map[string]string{"path": "content", "reason": "Only UTF-8 textual bodies up to 64 KiB are supported; unavailable content follows organization failure policy"}, map[string]string{"path": "content.retention", "reason": "Content is retained for 10 minutes; at most 4096 uploads per authenticated scope per retention window, with bounded admission probes; materialized event content is limited to 256 KiB and 128 distinct references, with a one-second cache-read budget"}},
		"transports": []string{"http"}, "authentication": []string{"bearer"}, "toolPaths": []string{}, "contentCategories": []string{"text"}, "limits": map[string]any{"maxUploadBytes": ahpMaxContentBytes}, "managedPolicy": map[string]any{"scopes": []string{"project"}, "disableable": true}, "correlationIdentityFields": []string{"source", "id", "session.id", "turn.id", "call.id"},
	}})
	var result ahp.CapabilitiesResponseResult
	if err == nil {
		err = json.Unmarshal(b, &result)
	}
	if err != nil {
		return result, fmt.Errorf("encode AHP capabilities: %w", err)
	}
	return result, nil
}

//go:fix inline
func ptrAHP[T any](value T) *T { return new(value) }

// The source URI identifies a harness, not authorization. Strip accidental
// transport secrets while retaining a recognizable bounded display identity.
func ahpSourceLabel(source string) string {
	u, err := url.Parse(source)
	if err != nil {
		return "ahp-source:" + ahpIdentity(source)[:24]
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	label := u.String()
	if len(label) > 256 {
		label = label[:220] + "#" + ahpIdentity(source)[:24]
	}
	return label
}

type ahpFailureTrackerKey struct{}

func withAHPFailureTracker(ctx context.Context) (context.Context, *string) {
	failure := new(string)
	return context.WithValue(ctx, ahpFailureTrackerKey{}, failure), failure
}
func markAHPFailure(ctx context.Context, reason string) {
	options := authenticatedIngestOptions(ctx)
	if !options.AHPPolicy || options.ObserveOnly {
		return
	}
	if failure, ok := ctx.Value(ahpFailureTrackerKey{}).(*string); ok {
		*failure = reason
	}
	if options.SourceAttributes != nil {
		options.SourceAttributes[attr.Key("gram.hook.enforcement_gap")] = reason
	}
}

func boundedAHPLabel(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	if len(value) > 256 {
		return value[:220] + "#" + ahpIdentity(value)[:24]
	}
	return value
}

func ahpPrincipalIdentity(ctx context.Context, authCtx *contextvalues.AuthContext) string {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
		return actor.String()
	}
	return authCtx.UserID
}

func sanitizeAHPError(raw any) any {
	data, err := json.Marshal(raw)
	if err != nil || string(data) == "null" {
		return nil
	}
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil {
		return nil
	}
	safe := map[string]any{}
	for _, key := range []string{"class", "message", "code", "status"} {
		if value, ok := fields[key]; ok {
			switch value.(type) {
			case string, float64:
				safe[key] = value
			}
		}
	}
	return safe
}

func ahpHasResolvedText(items []json.RawMessage, resolved map[string]string) bool {
	for _, raw := range items {
		var item struct {
			Selection string `json:"selection"`
			Body      struct {
				Ref string `json:"ref"`
			} `json:"body"`
		}
		if json.Unmarshal(raw, &item) == nil && item.Selection == "body" {
			if _, ok := resolved[item.Body.Ref]; ok {
				return true
			}
		}
	}
	return false
}
