package hooks

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	hooksserver "github.com/speakeasy-api/gram/server/gen/http/hooks/server"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// Dispatch is the agent-forwarded hook endpoint. The device-agent daemon
// authenticates with its agent-scoped org_token (not a hooks key, which it never
// holds) and forwards a tool's hook event, vouching for the user's email. We
// resolve the project from the org (the override slug if supplied, else the org
// default), inject it into the auth context, and fan out to the same per-tool
// handlers the published hook scripts hit — so attribution, idempotency and
// recording are identical; only the credential and project resolution differ.
//
// The vouched user_email is trusted within the authenticated org, exactly as the
// per-tool endpoints already trust the payload's user_email. The org boundary
// (the agent's key) is the security boundary; see DNO-376 / ADR-0010.
func (s *Service) Dispatch(ctx context.Context, payload *gen.DispatchPayload) (*gen.DispatchHookResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.E(oops.CodeForbidden, nil, "agent hook dispatch: unauthorized")
	}

	projectID, err := s.resolveDispatchProject(ctx, authCtx.ActiveOrganizationID, payload.ProjectSlug)
	if err != nil {
		return nil, err
	}

	// The agent key is org-scoped (ProjectID nil). Inject the resolved project so
	// the reused per-tool handlers — which read authCtx.ProjectID — attribute the
	// event to the right project.
	scoped := *authCtx
	scoped.ProjectID = &projectID
	scopedCtx := contextvalues.SetAuthContext(ctx, &scoped)

	rawPayload, err := json.Marshal(payload.Payload)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "agent hook dispatch: unmarshalable payload")
	}
	email := payload.UserEmail

	switch payload.Tool {
	case "cursor":
		var body hooksserver.CursorRequestBody
		if err := json.Unmarshal(rawPayload, &body); err != nil || body.HookEventName == nil {
			return nil, oops.E(oops.CodeInvalid, err, "agent hook dispatch: invalid cursor payload")
		}
		p := hooksserver.NewCursorPayload(&body, nil, nil, payload.HookHostname, payload.IdempotencyKey)
		p.UserEmail = &email
		res, err := s.Cursor(scopedCtx, p)
		if err != nil {
			return nil, err
		}
		return &gen.DispatchHookResult{
			Permission:   res.Permission,
			UserMessage:  res.UserMessage,
			AgentMessage: res.AgentMessage,
		}, nil

	case "codex":
		var body hooksserver.CodexRequestBody
		if err := json.Unmarshal(rawPayload, &body); err != nil || body.HookEventName == nil {
			return nil, oops.E(oops.CodeInvalid, err, "agent hook dispatch: invalid codex payload")
		}
		p := hooksserver.NewCodexPayload(&body, nil, nil, payload.HookHostname, payload.IdempotencyKey)
		p.UserEmail = &email
		res, err := s.Codex(scopedCtx, p)
		if err != nil {
			return nil, err
		}
		// Codex speaks {decision, reason}; map to the normalized shape.
		return &gen.DispatchHookResult{
			Permission:   res.Decision,
			UserMessage:  res.Reason,
			AgentMessage: res.Reason,
		}, nil

	case "claude_code":
		var body hooksserver.ClaudeRequestBody
		if err := json.Unmarshal(rawPayload, &body); err != nil || body.HookEventName == nil {
			return nil, oops.E(oops.CodeInvalid, err, "agent hook dispatch: invalid claude payload")
		}
		p := hooksserver.NewClaudePayload(&body, nil, nil, payload.HookHostname, payload.IdempotencyKey)
		p.UserEmail = &email
		res, err := s.Claude(scopedCtx, p)
		if err != nil {
			return nil, err
		}
		return normalizeClaudeResult(res), nil

	default:
		return nil, oops.E(oops.CodeInvalid, nil, "agent hook dispatch: unknown tool %q", payload.Tool)
	}
}

// resolveDispatchProject picks the project an org-forwarded hook attributes to:
// the override slug when supplied (validated within the org), otherwise the org
// default — the first project by id ASC, which ListProjectsByOrganization
// already orders and which the rest of the system treats as the default
// (plugins.GenerateConfig.IsDefaultProject, projects.Service delete guard).
func (s *Service) resolveDispatchProject(ctx context.Context, orgID string, override *string) (uuid.UUID, error) {
	projects, err := projectsrepo.New(s.db).ListProjectsByOrganization(ctx, orgID)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "agent hook dispatch: list projects")
	}
	if len(projects) == 0 {
		return uuid.Nil, oops.E(oops.CodeInvalid, nil, "agent hook dispatch: org has no project to attribute to")
	}
	if override != nil {
		if slug := strings.TrimSpace(*override); slug != "" {
			for _, p := range projects {
				if p.Slug == slug {
					return p.ID, nil
				}
			}
			return uuid.Nil, oops.E(oops.CodeInvalid, nil, "agent hook dispatch: project_slug %q not found in org", slug)
		}
	}
	return projects[0].ID, nil
}

// normalizeClaudeResult maps Claude's richer result to the normalized decision.
// Claude blocks two ways: a top-level decision == "block" (UserPromptSubmit /
// PostToolUse / Stop) or hookSpecificOutput.permissionDecision == "deny"
// (PreToolUse). Anything else is allow. Best-effort for v1.
func normalizeClaudeResult(res *gen.ClaudeHookResult) *gen.DispatchHookResult {
	permission := "allow"
	if res.Decision != nil && strings.EqualFold(*res.Decision, "block") {
		permission = "deny"
	}
	if perm := claudePermissionDecision(res.HookSpecificOutput); perm != "" {
		permission = perm
	}
	return &gen.DispatchHookResult{
		Permission:   &permission,
		UserMessage:  res.Reason,
		AgentMessage: res.Reason,
	}
}

// claudePermissionDecision pulls hookSpecificOutput.permissionDecision
// (PreToolUse) and maps it: "deny" -> deny, "allow"/"ask" -> allow. "" means the
// field is absent, so the caller keeps the top-level decision.
func claudePermissionDecision(hookSpecificOutput any) string {
	if hookSpecificOutput == nil {
		return ""
	}
	raw, err := json.Marshal(hookSpecificOutput)
	if err != nil {
		return ""
	}
	var hso struct {
		PermissionDecision string `json:"permissionDecision"`
	}
	if err := json.Unmarshal(raw, &hso); err != nil || hso.PermissionDecision == "" {
		return ""
	}
	if strings.EqualFold(hso.PermissionDecision, "deny") {
		return "deny"
	}
	return "allow"
}
