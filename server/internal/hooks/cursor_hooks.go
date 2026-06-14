package hooks

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/cursor"
	agenttypes "github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// Cursor is the endpoint for Cursor hook events
func (s *Service) Cursor(ctx context.Context, payload *gen.CursorPayload) (*gen.CursorHookResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		s.logger.WarnContext(ctx, "rejected unauthorized cursor hook request",
			attr.SlogEvent("cursor_hook_unauthorized"),
		)
		return &gen.CursorHookResult{
			Permission:        new("deny"),
			UserMessage:       new("Speakeasy hooks: unauthorized — check your Gram API key and project slug."),
			AdditionalContext: nil,
			AgentMessage:      nil,
		}, nil
	}

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	userEmail := conv.PtrValOr(payload.UserEmail, "")
	conversationID := conv.PtrValOr(payload.ConversationID, "")
	ev, err := agentevents.NewEvent(s.agentEvents,
		agentevents.EventContext{
			Provider:       cursor.Agent,
			OrgID:          orgID,
			ProjectID:      projectID,
			UserID:         authCtx.UserID,
			UserEmail:      userEmail,
			ConversationID: conversationID,
			Timestamp:      time.Now(),
		}, payload)
	if err != nil {
		return nil, err
	}

	eventType, ok, err := ev.EventType()
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(orgID),
		attr.SlogProjectID(projectID),
	)

	logger.InfoContext(ctx, "cursor hook received",
		attr.SlogEvent("cursor_hook"),
	)

	result := &gen.CursorHookResult{
		Permission:        nil,
		UserMessage:       nil,
		AdditionalContext: nil,
		AgentMessage:      nil,
	}

	if !ok {
		logger.InfoContext(ctx, "cursor hook received illegal event type",
			attr.SlogEvent("cursor_hook_illegal_event_type"),
			attr.SlogHookEvent(payload.HookEventName),
		)
		return result, nil
	}

	// blockReason is empty unless this call is denied by the shadow-MCP guard.
	// It propagates into the ClickHouse log entry as gram.hook.block_reason so
	// the trace renders as "blocked" in dashboards.
	var blockReason string

	switch eventType {
	case agenttypes.MCPToolCallStarted:
		// beforeMCPExecution fires for MCP-routed (non-local) tool calls. Run
		// the risk scanner first (block-only today), then fall through to the
		// shadow-MCP guard so unapproved toolsets are still blocked.
		if scanResult := s.scanCursorEventForEnforcement(ctx, ev, projectID); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
			break
		}
		policy := s.lookupShadowMCPBlockingPolicy(ctx, projectID)
		if policy == nil {
			result.Permission = new("allow")
			break
		}
		toolName := strings.TrimPrefix(conv.PtrValOr(payload.ToolName, ""), "MCP:")
		evidence := cursorShadowMCPEvidence(payload)
		if detail, denied := s.enforceShadowMCPToolAccess(ctx, orgID, projectID, authCtx.UserID, payload.ToolInput, toolName, evidence); denied {
			logger.InfoContext(ctx, "denying cursor tool call: failed gram toolset validation",
				attr.SlogEvent("cursor_hook_denied"),
				attr.SlogHookBlockReason(detail),
				attr.SlogRiskPolicyID(policy.ID),
				attr.SlogRiskPolicyName(policy.Name),
			)
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
			requesterUserID := authCtx.UserID
			if requesterUserID == "" {
				requesterUserID = s.resolveUserByEmail(ctx, conv.PtrValOr(payload.UserEmail, ""), orgID)
			}
			userReason := s.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
				OrganizationID:  orgID,
				ProjectID:       projectID,
				RequesterUserID: requesterUserID,
				UserMessage:     policy.UserMessage,
				AuditReason:     auditReason,
				Evidence:        evidence,
				ToolName:        toolName,
				ToolInput:       payload.ToolInput,
				RiskPolicyID:    policy.ID,
			})
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
			result.AgentMessage = &userReason
		} else {
			result.Permission = new("allow")
		}
	case agenttypes.ToolCallStarted:
		// preToolUse fires for ALL Cursor tool calls including MCP ones, while
		// beforeMCPExecution also fires for MCP-routed calls and already runs
		// the scan there. Skip the scan here for MCP tools to avoid scanning
		// (and DB-querying) the same input twice on the hot path. Native tools
		// (read_file, edit_file, ...) only have this single event and still
		// get scanned.
		toolName := conv.PtrValOr(payload.ToolName, "")
		if strings.HasPrefix(toolName, "MCP:") {
			result.Permission = new("allow")
			break
		}
		if scanResult := s.scanCursorEventForEnforcement(ctx, ev, projectID); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
		} else {
			result.Permission = new("allow")
		}
	case agenttypes.UserPromptSubmit:
		if scanResult := s.scanCursorEventForEnforcement(ctx, ev, projectID); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
		}
	default:
		// nothing to do
	}

	// Record the hook (will route to ClickHouse for tool calls, PG for all events).
	// Runs after the deny decision so the ClickHouse entry can carry the
	// block reason as an attribute.
	s.recordCursorHook(ctx, ev, blockReason)

	return result, nil
}

func (s *Service) recordCursorHook(ctx context.Context, ev agentevents.Event[*gen.CursorPayload], blockReason string) {
	payload := ev.Raw
	if payload == nil {
		s.logger.WarnContext(ctx, "Cursor event called without payload")
		return
	}
	if payload.ConversationID == nil || *payload.ConversationID == "" {
		s.logger.WarnContext(ctx, "Cursor event called without conversation ID")
		return
	}

	// Persistence outlives the request: the client may close the connection
	// the instant the hook returns, which would otherwise cancel in-flight
	// INSERTs and drop the chat message.
	ctx = context.WithoutCancel(ctx)

	userEmail := conv.PtrValOr(payload.UserEmail, "")
	userID := s.resolveUserByEmail(ctx, userEmail, ev.Context.OrgID)

	// Persistence does DB + ClickHouse writes that can take longer than the
	// client is willing to wait for a hook response (`stop` especially —
	// curl in send_hook.sh has a 10s --max-time and the client closes the
	// connection the moment the response lands). Run detached so the
	// response returns promptly and the work completes in the background.
	ev.Context.UserID = userID
	ev.Context.UserEmail = userEmail
	ev.Context.ConversationID = *payload.ConversationID
	ev.Context.Timestamp = time.Now()
	ev = ev.WithBlockReason(blockReason)

	go func() {
		if err := ev.Write(ctx); err != nil {
			s.logger.ErrorContext(ctx, "failed to write cursor agent event", attr.SlogError(err))
		}
	}()
}

// cursorMCPToolSource derives a tool_source string for beforeMCPExecution /
// afterMCPExecution events. URL-based servers use the URL host; command-based
// servers fall back to the command string.
func cursorMCPToolSource(payload *gen.CursorPayload) string {
	if payload.URL != nil && *payload.URL != "" {
		if u, err := url.Parse(*payload.URL); err == nil && u.Host != "" {
			return u.Host
		}
		return *payload.URL
	}
	if payload.Command != nil && *payload.Command != "" {
		return *payload.Command
	}
	return ""
}
