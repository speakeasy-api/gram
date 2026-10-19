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

const selectAssistantRuntimeToolName = "select_assistant_runtime"

// Optional capability preserves unavailable registration for incomplete compositions.
type assistantRuntimeManagement interface {
	GetAssistant(context.Context, *genassistants.GetAssistantPayload) (*types.Assistant, error)
	UpdateAssistant(context.Context, *genassistants.UpdateAssistantPayload) (*types.Assistant, error)
}

type SelectAssistantRuntimeInput struct {
	ProjectID              string `json:"project_id" jsonschema:"exact project UUID; never inferred"`
	AssistantID            string `json:"assistant_id" jsonschema:"exact assistant UUID"`
	RuntimeVersion         int    `json:"runtime_version" jsonschema:"2 for legacy runtime or 3 to opt into Kit"`
	ExpectedRuntimeVersion int    `json:"expected_runtime_version,omitempty" jsonschema:"persisted version from a fresh assistant read: 2 or 3; omit for preview; on conflict read again and reconfirm"`
	Confirmed              bool   `json:"confirmed" jsonschema:"false previews current runtime; true only after explicit confirmation of this exact assistant, project and runtime choice"`
}

type SelectAssistantRuntimeOutput struct {
	ProjectID      string `json:"project_id"`
	AssistantID    string `json:"assistant_id"`
	RuntimeVersion int    `json:"runtime_version"`
}

func (s *AssistantIdentityService) selectRuntime(ctx context.Context, principal Principal, input SelectAssistantRuntimeInput) (SelectAssistantRuntimeOutput, error) {
	var zero SelectAssistantRuntimeOutput
	if s == nil || s.management == nil || s.resolveProject == nil || s.organizations == nil {
		return zero, ErrUnavailable
	}
	management, ok := s.management.(assistantRuntimeManagement)
	if !ok {
		return zero, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil || projectID == uuid.Nil {
		return zero, oops.C(oops.CodeBadRequest)
	}
	assistantID, err := uuid.Parse(input.AssistantID)
	if err != nil || assistantID == uuid.Nil || (input.RuntimeVersion != 2 && input.RuntimeVersion != 3) || (input.Confirmed && (input.ExpectedRuntimeVersion < 2 || input.ExpectedRuntimeVersion > 3)) {
		return zero, oops.C(oops.CodeBadRequest)
	}
	// Preserve the canonical actor, session and live grants established by the
	// external runtime. Never manufacture a user identity from tool arguments.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || principal.UserID == "" || principal.OrganizationID == "" || authCtx.UserID != principal.UserID || authCtx.ActiveOrganizationID != principal.OrganizationID {
		return zero, oops.C(oops.CodeUnauthorized)
	}
	project, err := s.resolveProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: projectID.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return zero, err
	}
	if project.ID != projectID {
		return zero, ErrForbidden
	}
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil {
		return zero, fmt.Errorf("%w: resolve organization slug for assistant identity: %w", ErrUnavailable, err)
	}
	if organizationSlug == "" {
		return zero, fmt.Errorf("%w: organization slug is unavailable", ErrUnavailable)
	}
	scoped := *authCtx
	scoped.OrganizationSlug = organizationSlug
	scoped.ProjectID = &project.ID
	scoped.ProjectSlug = &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, &scoped)

	current, err := management.GetAssistant(ctx, &genassistants.GetAssistantPayload{ID: assistantID.String(), SessionToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return zero, fmt.Errorf("read assistant runtime selection: %w", err)
	}
	if current == nil || current.ID != assistantID.String() || current.ProjectID != projectID.String() {
		return zero, ErrUnavailable
	}
	if !input.Confirmed {
		return SelectAssistantRuntimeOutput{ProjectID: current.ProjectID, AssistantID: current.ID, RuntimeVersion: current.RuntimeVersion}, nil
	}
	// The authorized service performs the locked compare-and-set and audits the
	// persisted change. No frontend flag grants access.
	assistant, err := management.UpdateAssistant(ctx, &genassistants.UpdateAssistantPayload{
		ID: assistantID.String(), RuntimeVersion: &input.RuntimeVersion, ExpectedRuntimeVersion: &input.ExpectedRuntimeVersion,
		SessionToken: nil, ProjectSlugInput: nil, Name: nil, Model: nil, Instructions: nil, Toolsets: nil, McpServers: nil,
		WarmTTLSeconds: nil, MaxConcurrency: nil, Status: nil,
	})
	if err != nil {
		return zero, fmt.Errorf("select assistant runtime: %w", err)
	}
	if assistant == nil || assistant.ID != assistantID.String() || assistant.ProjectID != projectID.String() {
		return zero, ErrUnavailable
	}
	return SelectAssistantRuntimeOutput{ProjectID: assistant.ProjectID, AssistantID: assistant.ID, RuntimeVersion: assistant.RuntimeVersion}, nil
}

func registerAssistantRuntimeTool(reg *Registrar, service *AssistantIdentityService) {
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:        selectAssistantRuntimeToolName,
		Title:       "Select Assistant Runtime",
		Description: "Select the persisted runtime for one assistant in an exact project. Versions 1 and 2 are legacy; select 2 for legacy or explicitly opt into Kit with 3. First call with confirmed:false to preview the current runtime_version without changing it, then obtain explicit confirmation of the exact target and choice. Supply that value as expected_runtime_version. Requires organization administrator access and the same project:write authorization as the dashboard; a frontend feature flag is not authorization. The shared service rejects stale versions and unsettled active or background work. On conflict read again and reconfirm; never automatically overwrite. Returns committed runtime configuration, not runtime readiness, instructions or credentials. Managed assistants cannot change their own runtime through this tool.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), ReadOnlyHint: false, IdempotentHint: false, OpenWorldHint: nil, Title: ""},
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: nil}, func(ctx context.Context, _ *mcp.CallToolRequest, input SelectAssistantRuntimeInput) (*mcp.CallToolResult, SelectAssistantRuntimeOutput, error) {
		return principalToolCall(ctx, assistantRuntimeToolResult, func(principal Principal) (SelectAssistantRuntimeOutput, error) {
			return service.selectRuntime(ctx, principal, input)
		})
	})
}

func assistantRuntimeToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "assistant_runtime", Message: "Assistant runtime selection is temporarily unavailable."}
	var shared *oops.ShareableError
	if errors.Is(err, ErrForbidden) {
		result.Code, result.Message = "not_found", "That assistant or project is not available to you."
	} else if errors.As(err, &shared) {
		switch shared.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact project and assistant UUIDs, a supported runtime, its freshly read expected version and explicit confirmation."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "Runtime selection requires project:write and a user identity."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That assistant or project is not available to you."
		case oops.CodeConflict:
			result.Code, result.Message = "conflict", "The runtime version changed or assistant work is not settled. Read the assistant again and reconfirm before retrying."
		default:
			// Unknown failures retain the bounded unavailable response.
		}
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Meta: nil, StructuredContent: nil, InputRequests: nil, RequestState: "", Content: []mcp.Content{&mcp.TextContent{Meta: nil, Annotations: nil, Text: string(payload)}}, IsError: true}, true
}
