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
	"github.com/speakeasy-api/gram/server/internal/feature"
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
	AssistantID string `json:"assistant_id" jsonschema:"exact UUID of the assistant to upgrade; repeating the upgrade for an assistant that already has a dedicated agent is safe"`
	Confirmed   bool   `json:"confirmed" jsonschema:"true only after the user explicitly confirms upgrading this exact assistant in this exact project"`
}

type UpgradeAssistantIdentityOutput struct {
	ProjectID     string  `json:"project_id"`
	AssistantID   string  `json:"assistant_id"`
	IdentityState *string `json:"identity_state,omitempty"`
	AgentID       *string `json:"agent_id,omitempty"`
}

// errAssistantIdentityNotEnabled reports an organization outside the
// assistant workload identity rollout.
var errAssistantIdentityNotEnabled = errors.New("assistant workload identity is not enabled for this organization")

// AssistantIdentityService backs upgrade_assistant_workload_identity.
type AssistantIdentityService struct {
	management     AssistantIdentityManagement
	resolveProject func(context.Context, string, FindMCPInput) (ResolvedProject, error)
	organizations  OrganizationSlugResolver
	flags          feature.Provider
}

// NewAssistantIdentityService resolves the named project through the same
// inventory read the other project-scoped tools use, and gates the upgrade on
// the same rollout flag as the management endpoint.
func NewAssistantIdentityService(management AssistantIdentityManagement, projects *PostgresReader, flags feature.Provider) *AssistantIdentityService {
	return &AssistantIdentityService{
		management:     management,
		resolveProject: projects.resolveInventoryProject,
		organizations:  NewPostgresOrganizationSlugResolver(projects.db),
		flags:          flags,
	}
}

func (s *AssistantIdentityService) upgrade(ctx context.Context, principal Principal, input UpgradeAssistantIdentityInput) (UpgradeAssistantIdentityOutput, error) {
	var zero UpgradeAssistantIdentityOutput
	if s == nil || s.management == nil || s.resolveProject == nil || s.organizations == nil {
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
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil {
		return zero, fmt.Errorf("%w: resolve organization slug for assistant identity: %w", ErrUnavailable, err)
	}
	if organizationSlug == "" {
		return zero, fmt.Errorf("%w: organization slug is unavailable", ErrUnavailable)
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagAgentIdentityCredentials, principal.OrganizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		return zero, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if evaluation != feature.EvaluationEnabled {
		return zero, errAssistantIdentityNotEnabled
	}
	scoped := *authCtx
	scoped.OrganizationSlug = organizationSlug
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
	return UpgradeAssistantIdentityOutput{ProjectID: assistant.ProjectID, AssistantID: assistant.ID, IdentityState: assistant.IdentityState, AgentID: assistant.AgentID}, nil
}

func registerAssistantIdentityTool(reg *Registrar, service *AssistantIdentityService) {
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:        upgradeAssistantIdentityToolName,
		Title:       "Upgrade Assistant Workload Identity",
		Description: "Explicitly give one assistant in an exact project its own dedicated agent and per-trigger workload identities. The agent starts with access to every MCP server and skill in the project and to administering this assistant, and can be refined like any other agent afterwards. Ask the user to confirm the exact project and assistant before setting confirmed: true. Requires organization administrator access and the same project:write authorization as the dashboard. Repeating the upgrade is safe but keeps the existing agent, so it does not replace one that was suspended, revoked, or deleted. Unavailable until assistant workload identity is enabled for the organization; when it is not, tell the user and do not retry. Returns only identity configuration state, never credentials, instructions, bindings, or policy. ACTIVE does not prove execution permission or runtime readiness.",
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
	case errors.Is(err, errAssistantIdentityNotEnabled):
		result.Message = "Assistant workload identity is not enabled for this organization."
	case errors.Is(err, ErrForbidden):
		result.Code, result.Message = "not_found", "That assistant or project is not available to you."
	case errors.As(err, &shareable):
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact project and assistant UUIDs and explicitly confirm this upgrade."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "This upgrade requires project:write and a user identity."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That assistant or project is not available to you."
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
