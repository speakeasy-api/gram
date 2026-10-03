package platformmcp

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	genassistants "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const inspectAssistantIdentityToolName = "inspect_assistant_workload_identity"

type InspectAssistantIdentityInput struct {
	ProjectID   string `json:"project_id" jsonschema:"exact project UUID owning the assistant"`
	AssistantID string `json:"assistant_id" jsonschema:"exact assistant UUID to inspect"`
}

type assistantIdentityReader interface {
	GetAssistant(context.Context, *genassistants.GetAssistantPayload) (*types.Assistant, error)
}

func (s *assistantIdentityService) inspect(ctx context.Context, principal Principal, input InspectAssistantIdentityInput) (UpgradeAssistantIdentityOutput, error) {
	var zero UpgradeAssistantIdentityOutput
	if s == nil || s.management == nil || s.resolveProject == nil {
		return zero, ErrUnavailable
	}
	reader, ok := s.management.(assistantIdentityReader)
	if !ok {
		return zero, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil || projectID == uuid.Nil {
		return zero, oops.C(oops.CodeBadRequest)
	}
	assistantID, err := uuid.Parse(input.AssistantID)
	if err != nil || assistantID == uuid.Nil {
		return zero, oops.C(oops.CodeBadRequest)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || principal.OrganizationID == "" || principal.UserID == "" || authCtx.ActiveOrganizationID != principal.OrganizationID || authCtx.UserID != principal.UserID {
		return zero, oops.C(oops.CodeUnauthorized)
	}
	project, err := s.resolveProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: projectID.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return zero, err
	}
	if project.ID != projectID {
		return zero, ErrForbidden
	}
	scoped := *authCtx
	scoped.ProjectID = &project.ID
	scoped.ProjectSlug = &project.Slug
	assistant, err := reader.GetAssistant(contextvalues.SetAuthContext(ctx, &scoped), &genassistants.GetAssistantPayload{ID: assistantID.String(), SessionToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return zero, fmt.Errorf("inspect assistant identity: %w", err)
	}
	return UpgradeAssistantIdentityOutput{ProjectID: assistant.ProjectID, AssistantID: assistant.ID, IdentityState: assistant.IdentityState, AgentID: assistant.AgentID, IdentityGeneration: assistant.IdentityGeneration, Diagnostics: assistant.IdentityDiagnostics}, nil
}

func registerAssistantIdentityInspectionTool(reg *Registrar, service *assistantIdentityService) {
	addTool(reg, &mcp.Tool{Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil, Name: inspectAssistantIdentityToolName, Title: "Inspect assistant workload identity", Description: "Inspect one exact assistant and project using the dashboard's project:read authorization. Returns safe identity health, trigger binding states, rollout gates, and the last captured execution mode/fallback. Identity mapping and configuration do not prove business permission or OAuth consent. Shared history is intentional; delegation is per message. Does not return instructions, credentials, policy contents, or mint tokens. Use upgrade_assistant_workload_identity for confirmed legacy upgrades or missing-root repair; use existing agent/workload controls for suspension or revoked authority.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""}}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: nil}, func(ctx context.Context, _ *mcp.CallToolRequest, input InspectAssistantIdentityInput) (*mcp.CallToolResult, UpgradeAssistantIdentityOutput, error) {
		return principalToolCall(ctx, assistantIdentityToolResult, func(p Principal) (UpgradeAssistantIdentityOutput, error) { return service.inspect(ctx, p, input) })
	})
}
