package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	genassistants "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const chooseAssistantUpstreamCredentialToolName = "choose_assistant_upstream_credential"

// Optional capability preserves unavailable registration for incomplete compositions.
type assistantExecutionProfileManagement interface {
	ListExecutionProfiles(context.Context, *genassistants.ListExecutionProfilesPayload) (*genassistants.ListExecutionProfilesResult, error)
	SetExecutionProfileChoice(context.Context, *genassistants.SetExecutionProfileChoicePayload) (*genassistants.AssistantExecutionProfile, error)
}

type ChooseAssistantUpstreamCredentialInput struct {
	ProjectID         string `json:"project_id" jsonschema:"exact project UUID; never inferred"`
	AssistantID       string `json:"assistant_id" jsonschema:"exact assistant UUID"`
	TriggerInstanceID string `json:"trigger_instance_id,omitempty" jsonschema:"root trigger UUID from a preview; required with confirmed:true"`
	ExecutionContext  string `json:"execution_context,omitempty" jsonschema:"user_backed for turns acting for a person, userless for turns acting for nobody; required with confirmed:true"`
	McpServerID       string `json:"mcp_server_id,omitempty" jsonschema:"MCP server UUID from a preview; required with confirmed:true"`
	AuthClientID      string `json:"auth_client_id,omitempty" jsonschema:"one of the server's options from a preview"`
	NoAuth            bool   `json:"no_auth,omitempty" jsonschema:"use no upstream authentication, for a server that needs none"`
	Automatic         bool   `json:"automatic,omitempty" jsonschema:"remove the choice so the next turn makes it from the server's configuration"`
	ExpectedVersion   int64  `json:"expected_version,omitempty" jsonschema:"the profile version from a fresh preview; on conflict preview again and reconfirm"`
	Confirmed         bool   `json:"confirmed" jsonschema:"false previews every profile; true only after explicit confirmation of this exact assistant, trigger, context, server and choice"`
}

type AssistantUpstreamCredentialOption struct {
	AuthClientID string `json:"auth_client_id"`
	Name         string `json:"name"`
	ClientType   string `json:"client_type"`
}

type AssistantUpstreamCredentialServer struct {
	McpServerID  string                              `json:"mcp_server_id"`
	Chosen       bool                                `json:"chosen"`
	AuthClientID string                              `json:"auth_client_id,omitempty"`
	Source       string                              `json:"source,omitempty"`
	GapReason    string                              `json:"gap_reason,omitempty"`
	Options      []AssistantUpstreamCredentialOption `json:"options"`
}

type AssistantUpstreamCredentialProfile struct {
	TriggerInstanceID string                              `json:"trigger_instance_id"`
	TriggerName       string                              `json:"trigger_name"`
	ExecutionContext  string                              `json:"execution_context"`
	Version           int64                               `json:"version"`
	Servers           []AssistantUpstreamCredentialServer `json:"servers"`
}

type ChooseAssistantUpstreamCredentialOutput struct {
	ProjectID   string                               `json:"project_id"`
	AssistantID string                               `json:"assistant_id"`
	Profiles    []AssistantUpstreamCredentialProfile `json:"profiles"`
}

func (s *AssistantIdentityService) chooseUpstreamCredential(ctx context.Context, principal Principal, input ChooseAssistantUpstreamCredentialInput) (ChooseAssistantUpstreamCredentialOutput, error) {
	var zero ChooseAssistantUpstreamCredentialOutput
	if s == nil || s.management == nil || s.resolveProject == nil || s.organizations == nil {
		return zero, ErrUnavailable
	}
	management, ok := s.management.(assistantExecutionProfileManagement)
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
	if input.Confirmed {
		modes := 0
		for _, set := range []bool{input.AuthClientID != "", input.NoAuth, input.Automatic} {
			if set {
				modes++
			}
		}
		_, triggerErr := uuid.Parse(input.TriggerInstanceID)
		_, serverErr := uuid.Parse(input.McpServerID)
		if modes != 1 || triggerErr != nil || serverErr != nil || (input.ExecutionContext != "user_backed" && input.ExecutionContext != "userless") {
			return zero, oops.C(oops.CodeBadRequest)
		}
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
		return zero, fmt.Errorf("%w: resolve organization slug for assistant credentials: %w", ErrUnavailable, err)
	}
	if organizationSlug == "" {
		return zero, fmt.Errorf("%w: organization slug is unavailable", ErrUnavailable)
	}
	scoped := *authCtx
	scoped.OrganizationSlug = organizationSlug
	scoped.ProjectID = &project.ID
	scoped.ProjectSlug = &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, &scoped)

	out := ChooseAssistantUpstreamCredentialOutput{ProjectID: project.ID.String(), AssistantID: assistantID.String(), Profiles: []AssistantUpstreamCredentialProfile{}}
	if !input.Confirmed {
		listed, err := management.ListExecutionProfiles(ctx, &genassistants.ListExecutionProfilesPayload{AssistantID: assistantID.String(), SessionToken: nil, ProjectSlugInput: nil})
		if err != nil {
			return zero, fmt.Errorf("read assistant upstream credentials: %w", err)
		}
		if listed == nil {
			return zero, ErrUnavailable
		}
		for _, profile := range listed.Profiles {
			out.Profiles = append(out.Profiles, assistantUpstreamCredentialProfile(profile))
		}
		return out, nil
	}
	// The authorized service locks the profile, compares the version, validates
	// the choice and audits it atomically, then returns committed state.
	payload := &genassistants.SetExecutionProfileChoicePayload{
		AssistantID: assistantID.String(), TriggerInstanceID: input.TriggerInstanceID, ExecutionContext: input.ExecutionContext,
		McpServerID: input.McpServerID, AuthClientID: nil, NoAuth: nil, Automatic: nil, ExpectedVersion: input.ExpectedVersion,
		SessionToken: nil, ProjectSlugInput: nil,
	}
	switch {
	case input.AuthClientID != "":
		payload.AuthClientID = &input.AuthClientID
	case input.NoAuth:
		payload.NoAuth = new(true)
	default:
		payload.Automatic = new(true)
	}
	profile, err := management.SetExecutionProfileChoice(ctx, payload)
	if err != nil {
		return zero, fmt.Errorf("choose assistant upstream credential: %w", err)
	}
	if profile == nil {
		return zero, ErrUnavailable
	}
	out.Profiles = append(out.Profiles, assistantUpstreamCredentialProfile(profile))
	return out, nil
}

func assistantUpstreamCredentialProfile(profile *genassistants.AssistantExecutionProfile) AssistantUpstreamCredentialProfile {
	out := AssistantUpstreamCredentialProfile{
		TriggerInstanceID: profile.TriggerInstanceID, TriggerName: profile.TriggerName, ExecutionContext: profile.ExecutionContext,
		Version: profile.Version, Servers: make([]AssistantUpstreamCredentialServer, 0, len(profile.Servers)),
	}
	for _, server := range profile.Servers {
		s := AssistantUpstreamCredentialServer{
			McpServerID: server.McpServerID, Chosen: server.Chosen, AuthClientID: "", Source: "", GapReason: "",
			Options: make([]AssistantUpstreamCredentialOption, 0, len(server.Options)),
		}
		if server.AuthClientID != nil {
			s.AuthClientID = *server.AuthClientID
		}
		if server.Source != nil {
			s.Source = *server.Source
		}
		if server.GapReason != nil {
			s.GapReason = *server.GapReason
		}
		for _, option := range server.Options {
			s.Options = append(s.Options, AssistantUpstreamCredentialOption{AuthClientID: option.AuthClientID, Name: option.Name, ClientType: option.ClientType})
		}
		out.Servers = append(out.Servers, s)
	}
	return out
}

func registerAssistantUpstreamCredentialTool(reg *Registrar, service *AssistantIdentityService) {
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:        chooseAssistantUpstreamCredentialToolName,
		Title:       "Choose Assistant Upstream Credential",
		Description: "See and choose which upstream credential one assistant in an exact project uses on each MCP server it reaches, per root trigger and per execution context (user_backed turns act for the person their event identified; userless turns act for nobody). First call with confirmed:false to preview every profile: each server's current choice, a gap_reason when it has none, and its options. Then obtain explicit confirmation of the exact trigger, context, server and choice, and call with confirmed:true, one of auth_client_id, no_auth or automatic, and the previewed version as expected_version. Userless turns can use shared credentials only. Turns already running keep their credential. Choosing grants nothing: each request still checks the turn may use the credential. Requires organization administrator access and assistant:write on the assistant. On conflict preview again and reconfirm; never automatically overwrite. Returns names and ids only, never credential values.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), ReadOnlyHint: false, IdempotentHint: false, OpenWorldHint: new(false), Title: ""},
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: nil}, func(ctx context.Context, _ *mcp.CallToolRequest, input ChooseAssistantUpstreamCredentialInput) (*mcp.CallToolResult, ChooseAssistantUpstreamCredentialOutput, error) {
		return principalToolCall(ctx, assistantUpstreamCredentialToolResult, func(principal Principal) (ChooseAssistantUpstreamCredentialOutput, error) {
			return service.chooseUpstreamCredential(ctx, principal, input)
		})
	})
}

func assistantUpstreamCredentialToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "assistant_upstream_credentials", Message: "Assistant upstream credentials are temporarily unavailable."}
	if errors.Is(err, ErrForbidden) {
		result.Code, result.Message = "not_found", "That assistant or project is not available to you."
	} else if shared, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shared.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact UUIDs from a fresh preview, one of auth_client_id, no_auth or automatic that the server offers for that context, the previewed version and explicit confirmation."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "Choosing an assistant's credentials requires organization administrator access, assistant:write on the assistant and a user identity."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That assistant, trigger, server or project is not available to you."
		case oops.CodeConflict:
			result.Code, result.Message = "conflict", "The assistant's credentials changed since the preview. Preview again and reconfirm before retrying."
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
