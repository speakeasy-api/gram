//nolint:exhaustruct // Optional MCP manifest and management payload fields use their zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// WorkloadAssertionReuseManagement preserves the management API's live scope,
// organization-tier visibility, row locking, and atomic audit checks.
type WorkloadAssertionReuseManagement interface {
	List(context.Context, *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error)
	UpdateIssuer(context.Context, *gen.UpdateIssuerPayload) (*gen.WorkloadIdentityPolicy, error)
}

type GetWorkloadAssertionReuseInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID establishing organization context; the policy applies organization-wide, not just to this project"`
	IssuerID  string `json:"issuer_id" jsonschema:"exact organization-tier workload issuer UUID; never a name or URL"`
}

type SetWorkloadAssertionReuseInput struct {
	ProjectID           string `json:"project_id" jsonschema:"exact project UUID establishing organization context; this does not limit the issuer-wide change to one project"`
	IssuerID            string `json:"issuer_id" jsonschema:"exact organization-tier workload issuer UUID"`
	AllowAssertionReuse bool   `json:"allow_assertion_reuse" jsonschema:"desired absolute policy value; true permits reuse of valid jti or uti assertions until expiry; false restores replay protection"`
	Confirmed           bool   `json:"confirmed" jsonschema:"true only after the user confirms this exact issuer, desired policy, and impact on every workload trusting this issuer across the organization"`
}

type WorkloadAssertionReuseOutput struct {
	ProjectID           string `json:"project_id"`
	OrganizationID      string `json:"organization_id"`
	IssuerID            string `json:"issuer_id"`
	IssuerName          string `json:"issuer_name"`
	PolicyScope         string `json:"policy_scope"`
	AllowAssertionReuse bool   `json:"allow_assertion_reuse"`
}

type WorkloadAssertionReuseService struct {
	management     WorkloadAssertionReuseManagement
	resolveProject func(context.Context, string, FindMCPInput) (ResolvedProject, error)
}

func (r *PostgresReader) WithWorkloadAssertionReuse(management WorkloadAssertionReuseManagement) *PostgresReader {
	r.workloadAssertionReuse = &WorkloadAssertionReuseService{management: management, resolveProject: r.resolveInventoryProject}
	return r
}

func (s *WorkloadAssertionReuseService) scopedContext(ctx context.Context, principal Principal, projectInput, issuerInput string) (context.Context, error) {
	if s == nil || s.management == nil || s.resolveProject == nil {
		return nil, ErrUnavailable
	}
	projectID, err := uuid.Parse(projectInput)
	if err != nil || projectID == uuid.Nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	issuerID, err := uuid.Parse(issuerInput)
	if err != nil || issuerID == uuid.Nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || principal.UserID == "" || principal.OrganizationID == "" || ac.UserID != principal.UserID || ac.ActiveOrganizationID != principal.OrganizationID {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	// Do not manufacture API-key tenancy to expose project-tier issuers to an
	// external user. The shared API intentionally restricts users to org tier.
	if _, byKey := contextvalues.APIKeyAuthorization(ctx); byKey {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	project, err := s.resolveProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: projectID.String()})
	if err != nil {
		return nil, err
	}
	if project.ID != projectID {
		return nil, ErrForbidden
	}
	scoped := *ac
	scoped.ProjectID, scoped.ProjectSlug = &project.ID, &project.Slug
	return contextvalues.SetAuthContext(ctx, &scoped), nil
}

func assertionReuseProjection(policy *gen.WorkloadIdentityPolicy, principal Principal, projectID, issuerID string) (WorkloadAssertionReuseOutput, error) {
	if policy != nil {
		for _, issuer := range policy.Issuers {
			if issuer != nil && issuer.ID == issuerID && issuer.OrganizationID == principal.OrganizationID && issuer.ProjectID == "" {
				return WorkloadAssertionReuseOutput{ProjectID: projectID, OrganizationID: principal.OrganizationID, IssuerID: issuer.ID, IssuerName: issuer.Name, PolicyScope: "organization", AllowAssertionReuse: issuer.AllowAssertionReuse}, nil
			}
		}
	}
	return WorkloadAssertionReuseOutput{}, oops.C(oops.CodeNotFound)
}

func (s *WorkloadAssertionReuseService) get(ctx context.Context, principal Principal, input GetWorkloadAssertionReuseInput) (WorkloadAssertionReuseOutput, error) {
	ctx, err := s.scopedContext(ctx, principal, input.ProjectID, input.IssuerID)
	if err != nil {
		return WorkloadAssertionReuseOutput{}, err
	}
	policy, err := s.management.List(ctx, &gen.ListPayload{})
	if err != nil {
		return WorkloadAssertionReuseOutput{}, err
	}
	return assertionReuseProjection(policy, principal, input.ProjectID, input.IssuerID)
}

func (s *WorkloadAssertionReuseService) set(ctx context.Context, principal Principal, input SetWorkloadAssertionReuseInput) (WorkloadAssertionReuseOutput, error) {
	if !input.Confirmed {
		return WorkloadAssertionReuseOutput{}, oops.C(oops.CodeBadRequest)
	}
	ctx, err := s.scopedContext(ctx, principal, input.ProjectID, input.IssuerID)
	if err != nil {
		return WorkloadAssertionReuseOutput{}, err
	}
	// A fresh read checks visibility and workload:read before the shared write
	// checks workload:write. Absolute assignment is idempotent, not a toggle.
	policy, err := s.management.List(ctx, &gen.ListPayload{})
	if err != nil {
		return WorkloadAssertionReuseOutput{}, err
	}
	if _, err := assertionReuseProjection(policy, principal, input.ProjectID, input.IssuerID); err != nil {
		return WorkloadAssertionReuseOutput{}, err
	}
	_, err = s.management.UpdateIssuer(ctx, &gen.UpdateIssuerPayload{ID: input.IssuerID, AllowAssertionReuse: &input.AllowAssertionReuse})
	if err != nil {
		return WorkloadAssertionReuseOutput{}, err
	}
	// Re-read after the audited transaction commits; never echo the input.
	policy, err = s.management.List(ctx, &gen.ListPayload{})
	if err != nil {
		return WorkloadAssertionReuseOutput{}, err
	}
	return assertionReuseProjection(policy, principal, input.ProjectID, input.IssuerID)
}

func registerWorkloadAssertionReuseTools(reg *Registrar, service *WorkloadAssertionReuseService) {
	addTool(reg, &mcp.Tool{Name: "get_workload_assertion_reuse", Description: "Read replay protection for one exact organization-tier workload issuer. Requires workload:read. The exact project establishes organization context, not policy scope. Returns safe configuration only, never assertions, tokens, keys, or admissions.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)}}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: []authz.Scope{authz.ScopeWorkloadRead}}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetWorkloadAssertionReuseInput) (*mcp.CallToolResult, WorkloadAssertionReuseOutput, error) {
		return principalToolCall(ctx, workloadAssertionReuseToolResult, func(p Principal) (WorkloadAssertionReuseOutput, error) { return service.get(ctx, p, input) })
	})
	addTool(reg, &mcp.Tool{Name: "set_workload_assertion_reuse", Description: "Set replay protection for one exact organization-tier workload issuer, without changing issuer trust, keys, or admissions. First read get_workload_assertion_reuse, then ask the user to confirm the exact issuer, desired value, and organization-wide impact before confirmed=true. Requires workload:read and workload:write. Enabling permits valid jti or uti assertions to be reused until expiry; disabling restores replay protection. Tokens without replay identifiers retain exact-token compatibility. This is an absolute, idempotent assignment, not a toggle or compare-and-swap: concurrent administrators use last committed write. Returns committed safe configuration, never credentials.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)}}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: []authz.Scope{authz.ScopeWorkloadWrite}}, func(ctx context.Context, _ *mcp.CallToolRequest, input SetWorkloadAssertionReuseInput) (*mcp.CallToolResult, WorkloadAssertionReuseOutput, error) {
		return principalToolCall(ctx, workloadAssertionReuseToolResult, func(p Principal) (WorkloadAssertionReuseOutput, error) { return service.set(ctx, p, input) })
	})
}

func workloadAssertionReuseToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "workload_assertion_reuse", Message: "Workload assertion reuse policy is temporarily unavailable."}
	var shareable *oops.ShareableError
	if errors.Is(err, ErrForbidden) {
		result.Code, result.Message = "not_found", "That project or issuer is not available to you."
	}
	if errors.As(err, &shareable) {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Provide exact project and issuer UUIDs and explicitly confirm an issuer-wide policy change."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "A user identity and workload:read are required; changes also require workload:write."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That project or issuer is not available to you."
		}
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}}, true
}
