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
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const upgradeAssistantIdentityToolName = "upgrade_assistant_workload_identity"

// AssistantIdentityManagement deliberately calls the same authorized endpoint
// as the dashboard, not the lower-level identity binding primitives.
type AssistantIdentityManagement interface {
	UpgradeAssistantIdentity(context.Context, *genassistants.UpgradeAssistantIdentityPayload) (*types.Assistant, error)
}

type UpgradeAssistantIdentityInput struct {
	ProjectID   string `json:"project_id" jsonschema:"exact project UUID owning the assistant; never inferred"`
	AssistantID string `json:"assistant_id" jsonschema:"exact assistant UUID for an explicitly requested legacy upgrade or missing-active-root repair; provisioning rollout must be enabled"`
	Confirmed   bool   `json:"confirmed" jsonschema:"true only after the user explicitly confirms upgrading or repairing missing active trigger roots for this exact assistant in this exact project"`
}

type UpgradeAssistantIdentityOutput struct {
	Outcome            *string                             `json:"outcome,omitempty" jsonschema:"Committed action outcome: upgraded, repaired, or unchanged; absent on inspection"`
	ProjectID          string                              `json:"project_id"`
	AssistantID        string                              `json:"assistant_id"`
	IdentityState      *string                             `json:"identity_state,omitempty"`
	AgentID            *string                             `json:"agent_id,omitempty"`
	IdentityGeneration *int64                              `json:"identity_generation,omitempty"`
	Diagnostics        *types.AssistantIdentityDiagnostics `json:"diagnostics,omitempty"`
}

type assistantIdentityService struct {
	management     AssistantIdentityManagement
	resolveProject func(context.Context, string, FindMCPInput) (ResolvedProject, error)
}

func (s *assistantIdentityService) upgrade(ctx context.Context, principal Principal, input UpgradeAssistantIdentityInput) (UpgradeAssistantIdentityOutput, error) {
	var zero UpgradeAssistantIdentityOutput
	if s == nil || s.management == nil || s.resolveProject == nil {
		return zero, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil || projectID == uuid.Nil {
		return zero, oops.C(oops.CodeBadRequest)
	}
	assistantID, err := uuid.Parse(input.AssistantID)
	if err != nil || assistantID == uuid.Nil || !input.Confirmed {
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
	scoped := *authCtx
	scoped.ProjectID = &project.ID
	scoped.ProjectSlug = &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, &scoped)
	// The shared endpoint enforces project:write and the ordinary actor policy.
	// Its transaction locks and reads the exact assistant, performs the one-way
	// idempotent upgrade, audits it atomically, then returns committed state.
	assistant, err := s.management.UpgradeAssistantIdentity(ctx, &genassistants.UpgradeAssistantIdentityPayload{ID: assistantID.String(), SessionToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return zero, fmt.Errorf("upgrade assistant workload identity: %w", err)
	}
	if assistant == nil || assistant.ID != assistantID.String() || assistant.ProjectID != projectID.String() {
		return zero, ErrUnavailable
	}
	return UpgradeAssistantIdentityOutput{Outcome: assistant.IdentityUpgradeOutcome, ProjectID: assistant.ProjectID, AssistantID: assistant.ID, IdentityState: assistant.IdentityState, AgentID: assistant.AgentID, IdentityGeneration: assistant.IdentityGeneration, Diagnostics: assistant.IdentityDiagnostics}, nil
}

func registerAssistantIdentityTool(reg *Registrar, service *assistantIdentityService) {
	registerAssistantIdentityInspectionTool(reg, service)
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:        upgradeAssistantIdentityToolName,
		Title:       "Upgrade Assistant Workload Identity",
		Description: "Explicitly upgrade one legacy assistant, or repair missing live trigger roots for an active identity, in an exact project. Provisioning rollout must be enabled. Ask the user to confirm the exact project and assistant before setting confirmed: true. Requires organization administrator access and the same project:write and ordinary actor authorization as the dashboard. Repeating an already active upgrade is safe and does not widen permissions; suspended or revoked authority must be managed through existing agent/workload controls; tombstoned identities cannot be restored. Returns safe identity state and bounded diagnostics, never credentials, instructions, or policy contents. ACTIVE does not prove execution permission or runtime readiness.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: nil, ReadOnlyHint: false, Title: ""},
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: nil}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpgradeAssistantIdentityInput) (*mcp.CallToolResult, UpgradeAssistantIdentityOutput, error) {
		return principalToolCall(ctx, assistantIdentityToolResult, func(principal Principal) (UpgradeAssistantIdentityOutput, error) {
			return service.upgrade(ctx, principal, input)
		})
	})
}

func assistantIdentityToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "assistant_workload_identity", Message: "Assistant workload identity upgrades are temporarily unavailable."}
	var shareable *oops.ShareableError
	switch {
	case errors.Is(err, assistantidentity.ErrProvisioningDisabled), errors.Is(err, assistantidentity.ErrRolloutDisabled):
		result.Code, result.Message = "rollout_disabled", "Assistant identity changes are disabled by rollout. Retry only after provisioning is enabled."
	case errors.Is(err, ErrForbidden):
		result.Code, result.Message = "not_found", "That assistant or project is not available to you."
	case errors.As(err, &shareable):
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact project and assistant UUIDs and explicitly confirm this upgrade."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "This upgrade requires project:write and an authorized ordinary actor."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That assistant or project is not available to you."
		case oops.CodeConflict:
			result.Code, result.Message = "conflict", "The assistant identity cannot be upgraded from its current state. Refresh the assistant before retrying."
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
