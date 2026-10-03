package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
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
	return UpgradeAssistantIdentityOutput{Outcome: nil, ProjectID: assistant.ProjectID, AssistantID: assistant.ID, IdentityState: assistant.IdentityState, AgentID: assistant.AgentID, IdentityGeneration: assistant.IdentityGeneration, Diagnostics: assistant.IdentityDiagnostics}, nil
}

func registerAssistantIdentityInspectionTool(reg *Registrar, service *assistantIdentityService) {
	addTool(reg, &mcp.Tool{Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil, Name: inspectAssistantIdentityToolName, Title: "Inspect assistant workload identity", Description: "Inspect one exact assistant and project using the dashboard's project:read authorization. Returns safe identity health, trigger binding states, rollout gates, and the last captured execution mode/fallback. Identity mapping and configuration do not prove business permission or OAuth consent. Shared history is intentional; delegation is per message. Does not return instructions, credentials, policy contents, or mint tokens. Use upgrade_assistant_workload_identity for confirmed legacy upgrades or missing-root repair; use existing agent/workload controls for suspension or revoked authority.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""}}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input InspectAssistantIdentityInput) (*mcp.CallToolResult, UpgradeAssistantIdentityOutput, error) {
		return principalToolCall(ctx, assistantIdentityInspectionToolResult, func(p Principal) (UpgradeAssistantIdentityOutput, error) { return service.inspect(ctx, p, input) })
	})
}

func assistantIdentityInspectionToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "assistant_workload_identity", Message: "Assistant workload identity inspection is temporarily unavailable."}
	var shareable *oops.ShareableError
	switch {
	case errors.Is(err, ErrForbidden):
		result.Code, result.Message = "not_found", "That assistant or project is not available to you."
	case errors.As(err, &shareable):
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact project and assistant UUIDs to inspect."
		case oops.CodeForbidden, oops.CodeUnauthorized, oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That assistant or project is not available to you."
		case oops.CodeConflict:
			result.Code, result.Message = "conflict", "The assistant identity cannot be inspected in its current state. Refresh the assistant before retrying."
		default:
			// Unknown failures retain the bounded unavailable response.
		}
	}
	// Never expose backend errors, assistant instructions or identity policy.
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Meta: nil, StructuredContent: nil, InputRequests: nil, RequestState: "", Content: []mcp.Content{&mcp.TextContent{Meta: nil, Annotations: nil, Text: string(payload)}}, IsError: true}, true
}
