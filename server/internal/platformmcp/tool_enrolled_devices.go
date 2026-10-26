package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type enrolledDeviceManagement interface {
	List(context.Context, *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error)
}

// ListEnrolledDevicesInput selects one agent's device metadata.
type ListEnrolledDevicesInput struct {
	// ProjectID establishes the live organization context.
	ProjectID string `json:"project_id" jsonschema:"exact project UUID establishing organization context"`

	// AgentID is the exact agent whose enrolled devices are requested.
	AgentID string `json:"agent_id" jsonschema:"exact agent UUID"`

	// Cursor continues a prior result for this agent and project.
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the same project and agent"`

	// Limit bounds the response size.
	Limit int `json:"limit,omitempty" jsonschema:"page size from 1 to 100; defaults to 50"`
}

// EnrolledDeviceMetadata omits device subjects, tags, keys, and token secrets.
type EnrolledDeviceMetadata struct {
	// ID identifies the device admission.
	ID string `json:"id"`

	// AgentID identifies the currently assigned agent.
	AgentID string `json:"agent_id"`

	// Name is the device's display label, when supplied.
	Name string `json:"name"`

	// CreatedAt is when the device was admitted.
	CreatedAt string `json:"created_at"`
}

// EnrolledDevicesOutput is a bounded metadata page in stable admission-ID order.
type EnrolledDevicesOutput struct {
	// Items carries no device credentials or external subjects.
	Items []EnrolledDeviceMetadata `json:"items"`

	// NextCursor continues the same agent's results.
	NextCursor *string `json:"next_cursor,omitempty"`
}

func (s *DeviceEnrollmentTokenService) listEnrolledDevices(ctx context.Context, principal Principal, input ListEnrolledDevicesInput) (EnrolledDevicesOutput, error) {
	ctx, err := s.scopedContext(ctx, principal, input.ProjectID)
	if err != nil {
		return EnrolledDevicesOutput{}, err
	}
	management, ok := s.management.(enrolledDeviceManagement)
	if !ok {
		return EnrolledDevicesOutput{}, ErrUnavailable
	}
	agentID, err := uuid.Parse(input.AgentID)
	if err != nil || agentID == uuid.Nil {
		return EnrolledDevicesOutput{}, oops.C(oops.CodeBadRequest)
	}
	input.AgentID = agentID.String()
	if input.Cursor != "" {
		id, err := uuid.Parse(input.Cursor)
		if err != nil || id == uuid.Nil {
			return EnrolledDevicesOutput{}, oops.C(oops.CodeBadRequest)
		}
		input.Cursor = id.String()
	}
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return EnrolledDevicesOutput{}, oops.C(oops.CodeBadRequest)
	}
	// The shared endpoint rechecks workload:read in the live scoped context.
	policy, err := management.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return EnrolledDevicesOutput{}, fmt.Errorf("list enrolled devices: %w", err)
	}
	items := make([]EnrolledDeviceMetadata, 0)
	for _, admission := range policy.Admissions {
		if admission.AgentID != input.AgentID || admission.IssuerName != "Speakeasy devices" || !strings.HasPrefix(admission.Subject, "device:") || admission.ProjectID != "" {
			continue
		}
		if admission.ID <= input.Cursor {
			continue
		}
		items = append(items, EnrolledDeviceMetadata{ID: admission.ID, AgentID: admission.AgentID, Name: admission.Name, CreatedAt: admission.CreatedAt})
	}
	slices.SortFunc(items, func(a, b EnrolledDeviceMetadata) int { return strings.Compare(a.ID, b.ID) })
	var next *string
	if len(items) > limit {
		items = items[:limit]
		lastID := items[len(items)-1].ID
		next = &lastID
	}
	return EnrolledDevicesOutput{Items: items, NextCursor: next}, nil
}

func registerEnrolledDeviceTools(reg *Registrar, service *DeviceEnrollmentTokenService) {
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:        "list_enrolled_devices",
		Title:       "List an Agent's Enrolled Devices",
		Description: "List a bounded page of headless devices currently enrolled as one exact agent. Requires workload:read and a live user identity; the project establishes organization context. Returns admission IDs, agent IDs, display labels, and enrollment dates, never credentials, device subjects, or tags. Enrollment credential issuance remains in the dashboard because it returns one-time secrets. Device removal also remains in the dashboard: the withdrawal API cannot bind confirmation to an expected agent assignment, so this tool does not promise safe confirmed removal. Use list_device_enrollment_tokens and revoke_device_enrollment_token for unredeemed bootstrap tokens.",
		Annotations: &mcp.ToolAnnotations{Title: "", IdempotentHint: true, ReadOnlyHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: []authz.Scope{authz.ScopeWorkloadRead}}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListEnrolledDevicesInput) (*mcp.CallToolResult, EnrolledDevicesOutput, error) {
		return principalToolCall(ctx, enrolledDeviceToolResult, func(principal Principal) (EnrolledDevicesOutput, error) {
			return service.listEnrolledDevices(ctx, principal, input)
		})
	})
}

func enrolledDeviceToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "enrolled_devices", Message: "Enrolled device metadata is temporarily unavailable."}
	if errors.Is(err, ErrForbidden) {
		result.Code, result.Message = "not_found", "That project or agent is not available to you."
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact project and agent IDs, a valid page cursor, and a page size from 1 to 100."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "A live user identity and workload:read are required."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That project or agent is not available to you."
		case oops.CodeParseError, oops.CodeConflict, oops.CodeUnsupportedMedia, oops.CodeMethodNotAllowed, oops.CodeRequestTooLarge, oops.CodeUnexpected, oops.CodeInvariantViolation, oops.CodeGatewayError, oops.CodeUnavailable, oops.CodeNotImplemented, oops.CodeInsufficientCredits, oops.CodeRateLimitExceeded, oops.CodeInferenceDisabled, oops.CodeCanceled, oops.CodeFailedPrecondition:
			// Other failures keep the bounded unavailable response.
		}
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Meta: nil, StructuredContent: nil, InputRequests: nil, RequestState: "", IsError: true, Content: []mcp.Content{&mcp.TextContent{Meta: nil,
		Annotations: nil, Text: string(payload)}}}, true
}
