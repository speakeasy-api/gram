package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	ahp "github.com/agenthooksprotocol/go-sdk"
	ahpserver "github.com/agenthooksprotocol/go-sdk/server"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ahpAdapter is the canonical hook source for AHP events that report no
// harness name.
const ahpAdapter = "ahp"

// AttachAHP mounts the Agent Hooks Protocol endpoint. AHP events are
// translated into canonical hook events and processed by the same pipeline as
// POST /rpc/hooks.ingest.
func AttachAHP(mux goahttp.Muxer, s *Service) error {
	h, err := s.ahpHandler()
	if err != nil {
		return err
	}
	mux.Handle("POST", "/ahp", h.ServeHTTP)
	return nil
}

func (s *Service) ahpHandler() (http.Handler, error) {
	h, err := ahpserver.NewHandler(ahpserver.Handlers{
		Intercept: ahpserver.InterceptHandlers{ //nolint:exhaustruct // unhandled events are rejected as unsupported
			ToolBefore: func(ctx context.Context, c ahpserver.InterceptContext[ahp.ToolBeforeEvent, ahp.ToolBeforeCapabilities]) (ahpserver.ToolBeforeReply, error) {
				var reply ahpserver.ToolBeforeReply
				canDeny := slices.Contains(c.Capabilities.Effects, ahp.ToolBeforeCapabilitiesEffectsValueItemDeny)
				reason, err := s.ingestAHP(ctx, ahpToolBeforePayload(c.Event), !canDeny)
				if err != nil || reason == "" {
					return reply, err
				}
				reply.Effects = []*ahp.ToolBeforeEffect{{DenyEffect: ahp.Some(ahpDeny(reason))}}
				return reply, nil
			},
			UserMessageInbound: func(ctx context.Context, c ahpserver.InterceptContext[ahp.UserMessageInboundEvent, ahp.UserMessageInboundCapabilities]) (ahpserver.UserMessageInboundReply, error) {
				var reply ahpserver.UserMessageInboundReply
				payload := ahpUserMessageInboundPayload(c.Event)
				if payload == nil {
					return reply, nil
				}
				canDeny := slices.Contains(c.Capabilities.Effects, ahp.UserMessageInboundCapabilitiesEffectsValueItemDeny)
				reason, err := s.ingestAHP(ctx, payload, !canDeny)
				if err != nil || reason == "" {
					return reply, err
				}
				reply.Effects = []*ahp.UserMessageInboundEffect{{DenyEffect: ahp.Some(ahpDeny(reason))}}
				return reply, nil
			},
			// Lifecycle notifications are not authoritative user input.
			TurnStart: func(context.Context, ahpserver.InterceptContext[ahp.TurnStartEvent, ahp.TurnStartCapabilities]) (ahpserver.TurnStartReply, error) {
				var reply ahpserver.TurnStartReply
				return reply, nil
			},
		},
		// Observations are recorded without policy evaluation: the harness
		// has already acted, so a block could not be enforced.
		Observe: ahpserver.ObserveHandlers{ //nolint:exhaustruct // unobserved events are acknowledged and dropped
			ToolBefore: func(ctx context.Context, c ahpserver.ObserveContext[ahp.ToolBeforeEvent]) error {
				_, err := s.ingestAHP(ctx, ahpToolBeforePayload(c.Event), true)
				return err
			},
			UserMessageInbound: func(ctx context.Context, c ahpserver.ObserveContext[ahp.UserMessageInboundEvent]) error {
				payload := ahpUserMessageInboundPayload(c.Event)
				if payload == nil {
					return nil
				}
				_, err := s.ingestAHP(ctx, payload, true)
				return err
			},
			ModelResponseAfter: func(ctx context.Context, c ahpserver.ObserveContext[ahp.ModelResponseAfterEvent]) error {
				// Sequential ingestion preserves envelope order. Only the original
				// response carries usage; transcript fanout has distinct replay keys.
				for _, payload := range ahpModelResponsePayloads(c.Event) {
					if _, err := s.ingestAHP(ctx, payload, true); err != nil {
						return err
					}
				}
				return nil
			},
			ToolAfter: func(ctx context.Context, c ahpserver.ObserveContext[ahp.ToolAfterEvent]) error {
				payload := ahpToolAfterPayload(c.Event)
				if payload == nil {
					return nil
				}
				_, err := s.ingestAHP(ctx, payload, true)
				return err
			},
			SessionStart: func(ctx context.Context, c ahpserver.ObserveContext[ahp.SessionStartEvent]) error {
				e := c.Event
				_, err := s.ingestAHP(ctx, ahpPayload(e.Source, e.Harness, e.ID, string(e.Type), e.Time, "session.started", ahp.Some(e.Session), ""), true)
				return err
			},
			SessionEnd: func(ctx context.Context, c ahpserver.ObserveContext[ahp.SessionEndEvent]) error {
				e := c.Event
				_, err := s.ingestAHP(ctx, ahpPayload(e.Source, e.Harness, e.ID, string(e.Type), e.Time, "session.ended", ahp.Some(e.Session), ""), true)
				return err
			},
			Any: nil,
		},
		Capabilities: nil,
	}, ahpserver.Options{MaxRequestBytes: 1 << 20, MaxResponseBytes: 64 << 10, Diagnostic: nil})
	if err != nil {
		return nil, fmt.Errorf("create AHP handler: %w", err)
	}
	return s.ahpAuthenticate(h), nil
}

// ahpAuthenticate accepts the hooks API key as a bearer token or Gram-Key
// header and selects the project from Gram-Project.
func (s *Service) ahpAuthenticate(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Gram-Key"))
		if scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " "); ok && strings.EqualFold(scheme, "Bearer") {
			key = strings.TrimSpace(token)
		}
		project := strings.TrimSpace(r.Header.Get("Gram-Project"))
		if key == "" || project == "" {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		ctx, err := s.authorizePluginRequest(r.Context(), key, project)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, errAgentHooksDenied) {
				status = http.StatusForbidden
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ingestAHP runs a translated event through canonical ingest and returns the
// deny reason, or "" when the event is allowed. observeOnly records the event
// without evaluating policy. Errors surface to the harness as protocol errors,
// so its registered failure policy decides the outcome.
func (s *Service) ingestAHP(ctx context.Context, payload *gen.IngestPayload, observeOnly bool) (string, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return "", errors.New("AHP request is not authenticated")
	}
	payload.IdempotencyKey = new(fmt.Sprintf("ahp:%s:%s", authCtx.ProjectID, *payload.IdempotencyKey))
	options := defaultAuthenticatedIngestOptions()
	options.ObserveOnly = observeOnly
	result, err := s.IngestAuthenticatedWithOptions(ctx, authCtx, payload, options)
	if err != nil {
		return "", err
	}
	if result.Decision != "deny" {
		return "", nil
	}
	return conv.PtrValOr(result.Message, "Request denied by Speakeasy policy."), nil
}

func ahpDeny(reason string) *ahp.DenyEffect {
	return &ahp.DenyEffect{Type: ahp.DenyEffectTypeDeny, Reason: reason, Code: ahp.Optional[string]{Value: "", Present: false}, Extensions: ahp.Optional[*ahp.Extensions]{Value: nil, Present: false}, AdditionalProperties: nil}
}

func ahpPayload(source string, harness *ahp.Harness, id, rawType, occurredAt, eventType string, session ahp.Optional[*ahp.Session], turnID string) *gen.IngestPayload {
	payload := &gen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Replayed:         nil,
		SchemaVersion:    hookIngestSchemaV1,
		IdempotencyKey:   new(source + ":" + id),
		Source:           &gen.HookIngestSource{Adapter: ahpAdapter, AdapterVersion: nil, RawEventName: &rawType, Hostname: nil, UserEmail: nil},
		Session:          nil,
		Event:            &gen.HookIngestEvent{Type: eventType, OccurredAt: &occurredAt},
		Data:             &gen.HookIngestData{Prompt: nil, ToolCall: nil, Mcp: nil, McpInventory: nil, McpInventoryCollected: nil, Usage: nil, Message: nil, Skill: nil, Notification: nil, McpAttribution: nil, PromptAttachments: nil},
		Raw:              nil,
	}
	if harness != nil && strings.TrimSpace(harness.Name) != "" {
		payload.Source.Adapter = strings.TrimSpace(harness.Name)
		payload.Source.AdapterVersion = conv.PtrEmpty(strings.TrimSpace(harness.Version))
	}
	if session.Present && session.Value != nil {
		s := session.Value
		payload.Session = &gen.HookIngestSession{ID: &s.ID, TurnID: conv.PtrEmpty(turnID), Cwd: conv.PtrEmpty(s.CWD.Value), Model: conv.PtrEmpty(s.Model.Value)}
	}
	return payload
}

func ahpToolBeforePayload(e *ahp.ToolBeforeEvent) *gen.IngestPayload {
	payload := ahpPayload(e.Source, e.Harness, e.ID, string(e.Type), e.Time, "tool.requested", e.Session, e.Turn.Value.ID)
	payload.Data.ToolCall = ahpToolCall(e.Call.ID, e.Tool)
	payload.Data.Mcp = ahpMCP(e.Tool)
	return payload
}

// ahpToolAfterPayload maps executed tool calls to completions or failures.
// Calls that never executed are not recorded.
func ahpToolAfterPayload(e *ahp.ToolAfterEvent) *gen.IngestPayload {
	if e.Execution == nil || !e.Execution.Executed.Present {
		return nil
	}
	eventType := "tool.failed"
	if e.Outcome == ahp.ToolAfterEventOutcomeOk {
		eventType = "tool.completed"
	}
	payload := ahpPayload(e.Source, e.Harness, e.ID, string(e.Type), e.Time, eventType, e.Session, e.Turn.Value.ID)
	call := ahpToolCall(e.Call.ID, e.Tool)
	call.Status = new(string(e.Outcome))
	if e.Error.Present && e.Error.Value != nil {
		call.Error = map[string]any{"class": e.Error.Value.Class, "message": e.Error.Value.Message}
	}
	if e.DurationMs.Present {
		if ms, err := e.DurationMs.Value.Float64(); err == nil {
			call.DurationMs = &ms
		}
	}
	if output := ahpText(e.Items); output != "" {
		call.Output = output
	}
	payload.Data.ToolCall = call
	payload.Data.Mcp = ahpMCP(e.Tool)
	return payload
}

// Only inbound user envelopes own prompt policy and transcript capture.
func ahpUserMessageInboundPayload(e *ahp.UserMessageInboundEvent) *gen.IngestPayload {
	var texts []string
	for _, message := range e.Message.Messages {
		if message.Role == "user" {
			if text := ahpPartsText(message.Parts, false); text != "" {
				texts = append(texts, text)
			}
		}
	}
	if len(texts) == 0 {
		return nil
	}
	payload := ahpPayload(e.Source, e.Harness, e.ID, string(e.Type), e.Time, "prompt.submitted", e.Session, e.Turn.Value.ID)
	payload.Data.Prompt = &gen.HookPromptData{Text: new(strings.Join(texts, "\n"))}
	return payload
}

// Fanout remains adapter-local, leaving canonical ingest unchanged.
func ahpModelResponsePayloads(e *ahp.ModelResponseAfterEvent) []*gen.IngestPayload {
	original := ahpPayload(e.Source, e.Harness, e.ID, string(e.Type), e.Time, "usage.reported", e.Session, e.Turn.Value.ID)
	if e.Usage.Present && e.Usage.Value != nil {
		u := e.Usage.Value
		// Cumulative snapshots must not be added to per-attempt usage.
		if u.Kind == "amount" && u.Scope == "attempt" {
			original.Data.Usage = &gen.HookUsageData{
				InputTokens: ahpTokenCount(u.InputTokens), OutputTokens: ahpTokenCount(u.OutputTokens),
				CacheReadTokens: ahpTokenCount(u.CacheReadTokens), CacheWriteTokens: ahpTokenCount(u.CacheWriteTokens),
				Cost: nil, LoopCount: nil, Status: nil,
			}
			if u.Cost.Present && u.Cost.Value.Currency == "USD" {
				if cost, err := u.Cost.Value.Amount.Float64(); err == nil {
					original.Data.Usage.Cost = &cost
				}
			}
		}
	}
	payloads := []*gen.IngestPayload{original}
	for ordinal, item := range e.Items {
		if item == nil || item.Role != ahp.ModelVisibleItemRoleAssistant {
			continue
		}
		text := ahpPartsText(item.Parts, true)
		if text == "" {
			continue
		}
		payload := ahpPayload(e.Source, e.Harness, e.ID, string(e.Type), e.Time, "assistant.responded", e.Session, e.Turn.Value.ID)
		// Encode boundaries so source IDs and message IDs cannot collide.
		identity, _ := json.Marshal([]any{e.Source, e.ID, canonicalSessionID(payload), ordinal, item.ID})
		payload.IdempotencyKey = new(fmt.Sprintf("response-text:%x", sha256.Sum256(identity)))
		payload.Data.Message = &gen.HookMessageData{Text: &text, Role: new("assistant"), DurationMs: nil}
		payloads = append(payloads, payload)
	}
	return payloads
}

func ahpTokenCount(value ahp.Optional[json.Number]) *int {
	if !value.Present {
		return nil
	}
	count, err := value.Value.Int64()
	if err != nil || count < 0 || int64(int(count)) != count {
		return nil
	}
	return new(int(count))
}

func ahpToolCall(callID string, tool *ahp.ExecutionEventTool) *gen.HookToolCallData {
	call := &gen.HookToolCallData{ID: &callID, Name: nil, Input: nil, Output: nil, Error: nil, IsInterrupt: nil, PermissionType: nil, DurationMs: nil, Status: nil}
	if tool == nil {
		return call
	}
	call.Name = &tool.Name
	if raw, err := json.Marshal(tool.Input); err == nil {
		var input any
		if json.Unmarshal(raw, &input) == nil {
			call.Input = input
		}
	}
	return call
}

func ahpMCP(tool *ahp.ExecutionEventTool) *gen.HookMCPData {
	if tool == nil || !tool.Mcp.Present || tool.Mcp.Value == nil {
		return nil
	}
	m := tool.Mcp.Value
	data := &gen.HookMCPData{ServerName: conv.PtrEmpty(m.Server.Name.Value), ServerIdentity: conv.PtrEmpty(m.Server.ID), URL: nil, Command: nil, ResultJSON: nil}
	if m.Connection.HTTP.Present {
		data.URL = conv.PtrEmpty(m.Connection.HTTP.Value.URL.Value)
	}
	return data
}

// ahpText joins the inline text bodies of model-visible items. Content passed
// by reference is not fetched.
func ahpText(items []*ahp.ModelVisibleItem) string {
	var texts []string
	for _, item := range items {
		if item == nil {
			continue
		}
		for _, part := range item.Parts {
			if part != nil && part.TextBodyPart.Present && part.TextBodyPart.Value != nil {
				texts = append(texts, part.TextBodyPart.Value.Text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

// Category is independent of representation: tool calls and reasoning can
// both be text. Only assistant transcript text uses this category allowlist;
// tool results remain evidence regardless of their category.
func ahpPartsText(parts []*ahp.ContentItem, assistant bool) string {
	var texts []string
	for _, part := range parts {
		if part == nil || !part.TextBodyPart.Present || part.TextBodyPart.Value == nil {
			continue
		}
		body := part.TextBodyPart.Value
		if assistant && body.Category.Present && body.Category.Value != "text" {
			continue
		}
		texts = append(texts, body.Text)
	}
	return strings.Join(texts, "\n")
}
