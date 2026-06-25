package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	codexevents "github.com/speakeasy-api/gram/server/internal/hookevents/adapters/codex"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) Codex(ctx context.Context, payload *gen.CodexPayload) (*gen.CodexHookResult, error) {
	logger := s.logger.With(
		attr.SlogHookSource("codex"),
		attr.SlogHookEvent(payload.HookEventName),
		attr.SlogToolName(conv.PtrValOr(payload.ToolName, "")),
		attr.SlogGenAIConversationID(conv.PtrValOr(payload.SessionID, "")),
	)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		logger.WarnContext(ctx, "rejected unauthorized codex hook request",
			attr.SlogEvent("codex_hook_unauthorized"),
		)
		return &gen.CodexHookResult{
			Decision: new("deny"),
			Reason:   new("Speakeasy hooks: unauthorized — check your Gram API key and project slug."),
		}, nil
	}

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	metadata := s.codexSessionMetadata(ctx, payload, orgID, projectID)
	if metadata.UserEmail == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "codex hook payload missing user_email")
	}
	logger = logger.With(
		attr.SlogOrganizationID(orgID),
		attr.SlogProjectID(projectID),
	)

	logger.InfoContext(ctx, "codex hook received",
		attr.SlogEvent("codex_hook"),
	)

	// Claim the per-invocation idempotency token before persistence. A retry
	// re-sends the same token: the decision still re-runs so the user stays
	// blocked, but tagging the context as a duplicate suppresses the duplicate
	// writes in recordCodexHook.
	if !s.claimHookIdempotency(ctx, conv.PtrValOr(payload.IdempotencyKey, "")) {
		ctx = withHookDuplicate(ctx)
	}

	var blockReason, userReason string

	hookEvent, err := codexevents.Normalize(authCtx, payload, hookevents.EventContext{
		OrganizationID: orgID,
		ProjectID:      *authCtx.ProjectID,
		User: hookevents.User{
			ID:    metadata.UserID,
			Email: metadata.UserEmail,
		},
	}, time.Now())
	if err != nil {
		return nil, fmt.Errorf("normalize codex hook event: %w", err)
	}

	if hookEvent != nil {
		switch ev := hookEvent.(type) {
		case *hookevents.BeforeToolUse:
			if scanResult := s.scanToolRequestForEnforcement(ctx, ev); scanResult != nil {
				blockReason = fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
				userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
				break
			}
			policy := s.lookupShadowMCPBlockingPolicy(ctx, orgID, projectID, metadata.UserID)
			if policy != nil {
				toolName := ev.ToolName
				evidence, matched := s.codexShadowMCPEvidence(ctx, payload)
				detail, denied := s.enforceShadowMCPToolAccess(ctx, orgID, projectID, metadata.UserID, policy.ID, ev.ToolInput, toolName, evidence)
				if !denied {
					// Toolset validation proves a valid x-gram-toolset-id was
					// echoed, but a shadow server's schema can coach the client
					// into copying one. The inventory snapshot pins where the
					// call actually routes — deny when it points at a non-Gram
					// target. Unmatched prefixes and missing snapshots stay
					// allowed: older plugin installs ship no inventory.
					if d := s.codexInventoryProvenanceDetail(ctx, matched, orgID); d != "" {
						if _, allowed := s.canBypassPolicy(ctx, orgID, metadata.UserID, policy.ID, evidence, toolName); !allowed {
							detail, denied = d, true
						}
					}
				}
				if denied {
					logger.InfoContext(ctx, "denying codex tool call: failed gram toolset validation",
						attr.SlogEvent("codex_hook_denied"),
						attr.SlogHookBlockReason(detail),
						attr.SlogRiskPolicyID(policy.ID),
						attr.SlogRiskPolicyName(policy.Name),
					)
					blockReason = fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
					userReason = s.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
						OrganizationID:  orgID,
						ProjectID:       projectID,
						RequesterUserID: metadata.UserID,
						UserMessage:     policy.UserMessage,
						AuditReason:     blockReason,
						Evidence:        evidence,
						ToolName:        toolName,
						ToolInput:       ev.ToolInput,
						RiskPolicyID:    policy.ID,
					})
				}
			}
		case *hookevents.PermissionRequest:
			if scanResult := s.scanPermissionRequestForEnforcement(ctx, ev); scanResult != nil {
				blockReason = fmt.Sprintf("Speakeasy blocked this permission request: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
				userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
			}
		case *hookevents.UserPromptSubmit:
			if scanResult := s.scanUserPromptForEnforcement(ctx, ev); scanResult != nil {
				blockReason = fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
				userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
			}
		default:
			// Non-blocking events: telemetry only.
		}
	}

	s.recordCodexHook(ctx, hookEvent, payload, metadata, blockReason)

	if blockReason != "" {
		// Return the Codex hook JSON shape (decision=deny + reason) so the
		// Codex CLI surfaces the block reason to the user. Returning a 4xx
		// here would hide the reason behind whatever generic message the
		// transport layer renders.
		return &gen.CodexHookResult{
			Decision: new("deny"),
			Reason:   &userReason,
		}, nil
	}

	return &gen.CodexHookResult{
		Decision: nil,
		Reason:   nil,
	}, nil
}

func (s *Service) recordCodexHook(ctx context.Context, hookEvent any, payload *gen.CodexPayload, metadata *SessionMetadata, blockReason string) {
	// Skip persistence for a redelivery (the token was claimed in Codex()).
	if s.isHookDuplicate(ctx) {
		return
	}

	if payload.HookEventName == "SessionStart" {
		s.captureCodexMCPListSnapshot(ctx, payload)
		if metadata.SessionID != "" && metadata.UserEmail != "" {
			if err := s.cache.Set(ctx, sessionCacheKey(metadata.SessionID), *metadata, 24*time.Hour); err != nil {
				s.logger.WarnContext(ctx, "failed to cache Codex session metadata",
					attr.SlogError(err),
					attr.SlogGenAIConversationID(metadata.SessionID),
				)
			}
		}
	} else {
		s.refreshMCPListTTL(ctx, metadata.SessionID)
	}

	if hookEvent == nil {
		return
	}
	if s.eventWriter == nil {
		return
	}
	if err := s.eventWriter.Write(ctx, hookEvent, metadata, WriteOptions{BlockReason: blockReason, SkipChat: false}); err != nil {
		s.logger.ErrorContext(ctx, "failed to persist Codex hook event", attr.SlogError(err))
	}
}

// captureCodexMCPListSnapshot parses the MCP inventory shipped by the Codex
// SessionStart hook script (additional_data.mcp_inventory_codex, the parsed
// output of `codex mcp list --json`) and caches it under
// sessionMCPListCacheKey, sharing the snapshot shape and cache key with the
// Claude flows so downstream matching and telemetry enrichment work
// unchanged.
func (s *Service) captureCodexMCPListSnapshot(ctx context.Context, payload *gen.CodexPayload) {
	if payload.SessionID == nil || *payload.SessionID == "" || payload.AdditionalData == nil {
		return
	}
	raw := payload.AdditionalData["mcp_inventory_codex"]
	if raw == nil {
		return
	}

	entries := ParseCodexMCPList(raw)
	if err := s.cache.Set(ctx, sessionMCPListCacheKey(*payload.SessionID), entries, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to cache Codex MCP list snapshot",
			attr.SlogEvent("codex_hook_mcp_list_cache_set_failed"),
			attr.SlogError(err),
			attr.SlogGenAIConversationID(*payload.SessionID),
		)
	}
}

func (s *Service) codexSessionMetadata(ctx context.Context, payload *gen.CodexPayload, orgID, projectID string) *SessionMetadata {
	metadata := &SessionMetadata{
		SessionID:   conv.PtrValOr(payload.SessionID, ""),
		ServiceName: "Codex",
		UserEmail:   strings.TrimSpace(conv.PtrValOr(payload.UserEmail, "")),
		UserID:      "",
		ClaudeOrgID: "",
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}

	if metadata.SessionID != "" {
		cached, err := s.getSessionMetadata(ctx, metadata.SessionID)
		if err == nil && cached.ServiceName == "Codex" && cached.GramOrgID == orgID && cached.ProjectID == projectID {
			if metadata.UserEmail == "" {
				metadata.UserEmail = cached.UserEmail
			}
		}
	}

	if metadata.UserEmail != "" {
		metadata.UserID = s.resolveUserByEmail(ctx, metadata.UserEmail, orgID)
	} else {
		metadata.UserID = ""
	}

	return metadata
}
