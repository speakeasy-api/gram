package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// DeviceEnrollmentTokenManagement retains the management API's workload scopes,
// agent ownership checks, atomic redemption exclusion, and audit transaction.
type DeviceEnrollmentTokenManagement interface {
	ListDeviceJoinTokens(context.Context, *gen.ListDeviceJoinTokensPayload) (*gen.WorkloadDeviceJoinTokenList, error)
	RevokeDeviceJoinToken(context.Context, *gen.RevokeDeviceJoinTokenPayload) (*gen.WorkloadDeviceJoinTokenSummary, error)
}

// ListDeviceEnrollmentTokensInput selects bounded, secret-free token metadata.
type ListDeviceEnrollmentTokensInput struct {

	// ProjectID establishes organization context; tokens belong to the organization.
	ProjectID string `json:"project_id" jsonschema:"exact project UUID establishing organization context, not a limit on token scope"`

	// AgentID optionally narrows the results to one agent.
	AgentID string `json:"agent_id,omitempty" jsonschema:"optional exact agent UUID"`

	// TokenID selects one issuance without its secret.
	TokenID string `json:"token_id,omitempty" jsonschema:"optional exact token UUID for a fresh pre-revocation read"`

	// Cursor is the opaque continuation returned by this operation.
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the same organization and filters"`

	// Limit bounds the number of returned token summaries.
	Limit int `json:"limit,omitempty" jsonschema:"page size from 1 to 200; defaults to 50"`
}

// RevokeDeviceEnrollmentTokenInput names a confirmed, exact issuance.
type RevokeDeviceEnrollmentTokenInput struct {

	// ProjectID establishes the organization in which the action is authorized.
	ProjectID string `json:"project_id" jsonschema:"exact project UUID establishing organization context"`

	// AgentID is the agent from the fresh token metadata read.
	AgentID string `json:"agent_id" jsonschema:"exact agent UUID from a fresh list_device_enrollment_tokens read"`

	// TokenID is also the stable idempotency identity for this monotonic revocation.
	TokenID string `json:"token_id" jsonschema:"exact token UUID to revoke; repeating this revocation cannot create a second action"`

	// ExpectedStatus prevents acting on a token redeemed since the confirmed read.
	ExpectedStatus string `json:"expected_status" jsonschema:"active or expired, as returned by the fresh metadata read"`

	// Confirmed requires the user's explicit confirmation of this token and impact.
	Confirmed bool `json:"confirmed" jsonschema:"true only after the user confirms this exact token and agent and understands that it will no longer enroll a device"`
}

// DeviceEnrollmentTokenMetadata deliberately excludes token secrets, hashes,
// creator identity, and the enrolled device's external subject.
type DeviceEnrollmentTokenMetadata struct {

	// ID identifies this issuance.
	ID string `json:"id"`

	// AgentID identifies the agent the issuance enrolls under.
	AgentID string `json:"agent_id"`

	// CreatedAt is the issuance time.
	CreatedAt string `json:"created_at"`

	// ExpiresAt is the enrollment deadline.
	ExpiresAt string `json:"expires_at"`

	// Status is active, expired, redeemed, or revoked.
	Status string `json:"status"`
}

// DeviceEnrollmentTokensOutput carries one bounded metadata page.
type DeviceEnrollmentTokensOutput struct {

	// Items contains no enrollment credentials.
	Items []DeviceEnrollmentTokenMetadata `json:"items"`

	// NextCursor is absent on the last page.
	NextCursor *string `json:"next_cursor,omitempty"`
}

// DeviceEnrollmentTokenService exposes management through a live human identity.
type DeviceEnrollmentTokenService struct {
	management     DeviceEnrollmentTokenManagement
	resolveProject func(context.Context, string, FindMCPInput) (ResolvedProject, error)
}

// WithDeviceEnrollmentTokens supplies the same service used by the dashboard.
func (r *PostgresReader) WithDeviceEnrollmentTokens(management DeviceEnrollmentTokenManagement) *PostgresReader {
	r.deviceEnrollmentTokens = &DeviceEnrollmentTokenService{management: management, resolveProject: r.resolveInventoryProject}
	return r
}

func (s *DeviceEnrollmentTokenService) scopedContext(ctx context.Context, principal Principal, projectInput string) (context.Context, error) {
	if s == nil || s.management == nil || s.resolveProject == nil {
		return nil, ErrUnavailable
	}
	projectID, err := uuid.Parse(projectInput)
	if err != nil || projectID == uuid.Nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || principal.UserID == "" || principal.OrganizationID == "" || ac.UserID != principal.UserID || ac.ActiveOrganizationID != principal.OrganizationID {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if _, byKey := contextvalues.APIKeyAuthorization(ctx); byKey {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	project, err := s.resolveProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: projectID.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return nil, fmt.Errorf("resolve device enrollment project: %w", err)
	}
	if project.ID != projectID {
		return nil, ErrForbidden
	}
	scoped := *ac
	scoped.ProjectID, scoped.ProjectSlug = &project.ID, &project.Slug
	return contextvalues.SetAuthContext(ctx, &scoped), nil
}

func deviceEnrollmentTokenMetadata(token *gen.WorkloadDeviceJoinTokenSummary) DeviceEnrollmentTokenMetadata {
	return DeviceEnrollmentTokenMetadata{ID: token.ID, AgentID: token.AgentID, CreatedAt: token.CreatedAt, ExpiresAt: token.ExpiresAt, Status: token.Status}
}

func (s *DeviceEnrollmentTokenService) list(ctx context.Context, principal Principal, input ListDeviceEnrollmentTokensInput) (DeviceEnrollmentTokensOutput, error) {
	ctx, err := s.scopedContext(ctx, principal, input.ProjectID)
	if err != nil {
		return DeviceEnrollmentTokensOutput{}, err
	}
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return DeviceEnrollmentTokensOutput{}, oops.C(oops.CodeBadRequest)
	}
	page, err := s.management.ListDeviceJoinTokens(ctx, &gen.ListDeviceJoinTokensPayload{
		TokenID: &input.TokenID, AgentID: &input.AgentID, Cursor: &input.Cursor, Limit: limit,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	if err != nil {
		return DeviceEnrollmentTokensOutput{}, fmt.Errorf("list device enrollment tokens: %w", err)
	}
	items := make([]DeviceEnrollmentTokenMetadata, 0, len(page.Items))
	for _, token := range page.Items {
		items = append(items, deviceEnrollmentTokenMetadata(token))
	}
	return DeviceEnrollmentTokensOutput{Items: items, NextCursor: page.NextCursor}, nil
}

func (s *DeviceEnrollmentTokenService) revoke(ctx context.Context, principal Principal, input RevokeDeviceEnrollmentTokenInput) (DeviceEnrollmentTokenMetadata, error) {
	if !input.Confirmed || (input.ExpectedStatus != "active" && input.ExpectedStatus != "expired") {
		return DeviceEnrollmentTokenMetadata{}, oops.C(oops.CodeBadRequest)
	}
	for _, value := range []*string{&input.TokenID, &input.AgentID} {
		id, err := uuid.Parse(*value)
		if err != nil || id == uuid.Nil {
			return DeviceEnrollmentTokenMetadata{}, oops.C(oops.CodeBadRequest)
		}
		*value = id.String()
	}
	ctx, err := s.scopedContext(ctx, principal, input.ProjectID)
	if err != nil {
		return DeviceEnrollmentTokenMetadata{}, err
	}
	read := &gen.ListDeviceJoinTokensPayload{TokenID: &input.TokenID, AgentID: &input.AgentID, Cursor: nil, Limit: 1, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}
	before, err := s.management.ListDeviceJoinTokens(ctx, read)
	if err != nil {
		return DeviceEnrollmentTokenMetadata{}, fmt.Errorf("read device enrollment token: %w", err)
	}
	if len(before.Items) != 1 || before.Items[0].ID != input.TokenID || before.Items[0].AgentID != input.AgentID {
		return DeviceEnrollmentTokenMetadata{}, oops.C(oops.CodeNotFound)
	}
	if before.Items[0].Status != "revoked" && before.Items[0].Status != input.ExpectedStatus {
		return DeviceEnrollmentTokenMetadata{}, oops.C(oops.CodeFailedPrecondition)
	}

	// The shared write checks workload:write and current agent ownership even

	// on retries. A concurrent redemption makes its conditional UPDATE fail.
	_, writeErr := s.management.RevokeDeviceJoinToken(ctx, &gen.RevokeDeviceJoinTokenPayload{ID: input.TokenID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	if writeErr != nil {
		shareable, ok := errors.AsType[*oops.ShareableError](writeErr)
		if !ok || shareable.Code != oops.CodeFailedPrecondition {
			return DeviceEnrollmentTokenMetadata{}, fmt.Errorf("revoke device enrollment token: %w", writeErr)
		}
	}

	// Only a committed revoked state counts as success, including an exact

	// replay. A token redeemed in the read/write gap remains a refusal.
	after, err := s.management.ListDeviceJoinTokens(ctx, read)
	if err != nil {
		return DeviceEnrollmentTokenMetadata{}, fmt.Errorf("verify device enrollment revocation: %w", err)
	}
	if len(after.Items) != 1 || after.Items[0].ID != input.TokenID || after.Items[0].AgentID != input.AgentID || after.Items[0].Status != "revoked" {
		return DeviceEnrollmentTokenMetadata{}, oops.C(oops.CodeFailedPrecondition)
	}
	return deviceEnrollmentTokenMetadata(after.Items[0]), nil
}

func registerDeviceEnrollmentTokenTools(reg *Registrar, service *DeviceEnrollmentTokenService) {
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Title: "", Icons: nil,
		Name:        "list_device_enrollment_tokens",
		Description: "Inspect bounded device enrollment token metadata across the organization's agents, optionally for one exact agent or token. Requires workload:read and a live user identity. The exact project establishes organization context, not token scope. Never returns enrollment secrets, hashes, creator identities, or device subjects. Issuing enrollment credentials remains a dashboard or management-API operation.",
		Annotations: &mcp.ToolAnnotations{Title: "", IdempotentHint: true, ReadOnlyHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: []authz.Scope{authz.ScopeWorkloadRead}}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListDeviceEnrollmentTokensInput) (*mcp.CallToolResult, DeviceEnrollmentTokensOutput, error) {
		return principalToolCall(ctx, deviceEnrollmentTokenToolResult, func(p Principal) (DeviceEnrollmentTokensOutput, error) { return service.list(ctx, p, input) })
	})
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Title: "", Icons: nil,
		Name:        "revoke_device_enrollment_token",
		Description: "Prevent one exact unredeemed token from enrolling a device. First read list_device_enrollment_tokens, then ask the user to confirm the token, agent, and effect before confirmed=true. Requires workload:read, workload:write, a live user identity, and ownership of the agent or agent:authorize. Re-reads current metadata, refuses changed status or a racing redemption, preserves atomic audit logging, and verifies committed revocation. Token ID is the stable idempotency identity: exact retries return the revoked metadata without another mutation. Does not remove an already-enrolled device or expose secrets.",
		Annotations: &mcp.ToolAnnotations{Title: "", ReadOnlyHint: false, IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: []authz.Scope{authz.ScopeWorkloadRead, authz.ScopeWorkloadWrite}}, func(ctx context.Context, _ *mcp.CallToolRequest, input RevokeDeviceEnrollmentTokenInput) (*mcp.CallToolResult, DeviceEnrollmentTokenMetadata, error) {
		return principalToolCall(ctx, deviceEnrollmentTokenToolResult, func(p Principal) (DeviceEnrollmentTokenMetadata, error) { return service.revoke(ctx, p, input) })
	})
}

func deviceEnrollmentTokenToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "device_enrollment_tokens", Message: "Device enrollment token management is temporarily unavailable."}
	if errors.Is(err, ErrForbidden) {
		result.Code, result.Message = "not_found", "That project or token is not available to you."
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact project, agent, and token IDs, a bounded page size, and confirmation with the expected active or expired status when revoking."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "A live user and workload:read are required. Revocation also requires workload:write and agent ownership or agent:authorize."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That project or token is not available to you."
		case oops.CodeParseError, oops.CodeConflict, oops.CodeUnsupportedMedia, oops.CodeMethodNotAllowed, oops.CodeRequestTooLarge, oops.CodeUnexpected, oops.CodeInvariantViolation, oops.CodeGatewayError, oops.CodeUnavailable, oops.CodeNotImplemented, oops.CodeInsufficientCredits, oops.CodeRateLimitExceeded, oops.CodeInferenceDisabled, oops.CodeCanceled:
			// Other failures keep the bounded unavailable response.
		case oops.CodeFailedPrecondition:
			result.Code, result.Message = "state_changed", "The token no longer has the confirmed state. Read its metadata again. A redeemed token cannot be revoked; manage its enrolled device separately."
		}
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Meta: nil, StructuredContent: nil, InputRequests: nil, RequestState: "", IsError: true, Content: []mcp.Content{&mcp.TextContent{Meta: nil,
		Annotations: nil, Text: string(payload)}}}, true
}
