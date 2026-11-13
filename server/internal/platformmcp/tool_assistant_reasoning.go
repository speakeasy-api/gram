package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	genassistants "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const setAssistantReasoningEffortToolName = "set_assistant_reasoning_effort"

type SetAssistantReasoningEffortInput struct {
	ProjectID       string `json:"project_id" jsonschema:"exact project UUID; never inferred"`
	AssistantID     string `json:"assistant_id" jsonschema:"exact assistant UUID"`
	ReasoningEffort string `json:"reasoning_effort" jsonschema:"reasoning effort: none, low, medium, or high"`
	Confirmed       bool   `json:"confirmed" jsonschema:"false previews the current preference; true only after confirmation of this exact assistant, project and reasoning effort"`
}
type SetAssistantReasoningEffortOutput struct {
	ProjectID       string `json:"project_id"`
	AssistantID     string `json:"assistant_id"`
	ReasoningEffort string `json:"reasoning_effort"`
	RuntimeVersion  int    `json:"runtime_version"`
}

func (s *AssistantIdentityService) setReasoningEffort(ctx context.Context, principal Principal, input SetAssistantReasoningEffortInput) (SetAssistantReasoningEffortOutput, error) {
	var zero SetAssistantReasoningEffortOutput
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
	if err != nil || assistantID == uuid.Nil || (input.ReasoningEffort != "none" && input.ReasoningEffort != "low" && input.ReasoningEffort != "medium" && input.ReasoningEffort != "high") {
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
		return zero, fmt.Errorf("read assistant reasoning effort: %w", err)
	}
	if current == nil || current.ID != assistantID.String() || current.ProjectID != projectID.String() {
		return zero, ErrUnavailable
	}
	if !input.Confirmed {
		return SetAssistantReasoningEffortOutput{ProjectID: current.ProjectID, AssistantID: current.ID, ReasoningEffort: current.ReasoningEffort, RuntimeVersion: current.RuntimeVersion}, nil
	}
	// The authorized service locks the assistant and audits the persisted
	// change. No frontend flag grants access.
	assistant, err := management.UpdateAssistant(ctx, &genassistants.UpdateAssistantPayload{
		ID: assistantID.String(), ReasoningEffort: &input.ReasoningEffort, RuntimeVersion: nil, ExpectedRuntimeVersion: nil,
		SessionToken: nil, ProjectSlugInput: nil, Name: nil, Model: nil, Instructions: nil, Toolsets: nil, McpServers: nil,
		WarmTTLSeconds: nil, MaxConcurrency: nil, Status: nil,
	})
	if err != nil {
		return zero, fmt.Errorf("set assistant reasoning effort: %w", err)
	}
	if assistant == nil || assistant.ID != assistantID.String() || assistant.ProjectID != projectID.String() {
		return zero, ErrUnavailable
	}
	return SetAssistantReasoningEffortOutput{ProjectID: assistant.ProjectID, AssistantID: assistant.ID, ReasoningEffort: assistant.ReasoningEffort, RuntimeVersion: assistant.RuntimeVersion}, nil
}

func registerAssistantReasoningEffortTool(reg *Registrar, service *AssistantIdentityService) {
	addTool(reg, &mcp.Tool{
		Meta: nil, OutputSchema: nil, Icons: nil,
		InputSchema: closedObject(map[string]*jsonschema.Schema{
			"project_id":       uuidSchema("Exact project UUID; never inferred."),
			"assistant_id":     uuidSchema("Exact assistant UUID."),
			"reasoning_effort": {Type: "string", Enum: []any{"none", "low", "medium", "high"}},
			"confirmed":        {Type: "boolean", Description: "False previews; true requires explicit confirmation of the exact target and reasoning effort."},
		}, []string{"project_id", "assistant_id", "reasoning_effort", "confirmed"}),
		Name:        setAssistantReasoningEffortToolName,
		Title:       "Set an Assistant's Reasoning Effort",
		Description: "Configure reasoning effort for one assistant in an exact project: none, low, medium, or high. The none setting is only available on the default runtime (2) with compatible models; Kit (3) requires low, medium, or high. First call with confirmed:false to inspect the current preference and runtime_version, then obtain explicit confirmation of the exact assistant and desired value before confirmed:true. If the current runtime is Kit, choose low, medium, or high, or explicitly switch to the default runtime before setting none. Requires organization administrator access and assistant:write authorization. Omits all unrelated configuration, preserving model, runtime and attachments. Returns committed configuration only, not inference support or runtime readiness. Managed assistants cannot change their own configuration through this tool.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), ReadOnlyHint: false, IdempotentHint: true, OpenWorldHint: new(false), Title: ""},
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: nil}, func(ctx context.Context, _ *mcp.CallToolRequest, input SetAssistantReasoningEffortInput) (*mcp.CallToolResult, SetAssistantReasoningEffortOutput, error) {
		return principalToolCall(ctx, assistantReasoningEffortToolResult, func(principal Principal) (SetAssistantReasoningEffortOutput, error) {
			return service.setReasoningEffort(ctx, principal, input)
		})
	})
}

func assistantReasoningEffortToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "assistant_reasoning_effort", Message: "Assistant reasoning configuration is temporarily unavailable."}
	if errors.Is(err, ErrForbidden) {
		result.Code, result.Message = "not_found", "That assistant or project is not available to you."
	} else if shared, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shared.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact project and assistant UUIDs, a supported reasoning effort and explicit confirmation. Kit requires low, medium, or high; none is only available on the default runtime with compatible models. Preview the runtime and choose a supported effort or explicitly switch runtimes first."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "Reasoning configuration requires organization administrator access, assistant:write on the selected assistant and a user identity."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That assistant or project is not available to you."
		case oops.CodeConflict:
			result.Code, result.Message = "conflict", "Assistant configuration conflicted. Read the assistant again and reconfirm before retrying."
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
