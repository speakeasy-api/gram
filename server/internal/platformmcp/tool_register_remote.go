package platformmcp

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RegisterRemoteMCPToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that will own the remote MCP source"`
	ProbeReceipt   string `json:"probe_receipt" jsonschema:"server-issued probe receipt returned by probe_remote_mcp after the user explicitly confirmed its evidence; this tool never accepts a URL"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated idempotency key; reuse only to retry the same project and probed server"`
	DisplayName    string `json:"display_name,omitempty" jsonschema:"optional display name for the registered server; defaults to the probed server's host"`
}

type RegisterRemoteMCPToolOutput struct {
	ProjectSlug    string `json:"project_slug"`
	RemoteURL      string `json:"remote_url"`
	ReceiptID      string `json:"receipt_id"`
	RegistrationID string `json:"registration_id"`
	Replayed       bool   `json:"replayed"`

	// ProviderKey and CatalogRef are the registration's server-owned identity,
	// which downstream lifecycle tools such as get_setup_handoff accept to
	// return this server's Authentication settings dashboard link.
	ProviderKey string `json:"provider_key"`
	CatalogRef  string `json:"catalog_ref"`

	// BlockedPendingApproval reports that organization MCP approval enforcement
	// is active and this server is not approved under it. The registration
	// stands, but the server stays blocked until an admin approves it at
	// DashboardApprovalsURL.
	BlockedPendingApproval bool   `json:"blocked_pending_approval"`
	DashboardApprovalsURL  string `json:"dashboard_approvals_url,omitempty"`

	NextAction string `json:"next_action"`
	Message    string `json:"message"`
}

func registerRegisterRemoteMCPTool(reg *Registrar, registrations *RegistrationService, surfaceGate Gate, onboarding *OnboardingService) {
	addTool(reg, registerRemoteMCPToolManifest(), ToolMeta{
		// External-only: the probe receipt binds to the probing OAuth
		// connection, which a connection-less surface cannot present.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RegisterRemoteMCPToolInput) (*mcp.CallToolResult, RegisterRemoteMCPToolOutput, error) {
		var zero RegisterRemoteMCPToolOutput
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, zero, err
		}
		if refusal, ok := remoteMCPSurfaceRefusal(ctx, surfaceGate, principal); ok {
			return refusal, zero, nil
		}
		result, err := registrations.RegisterRemoteMCP(ctx, principal, RegisterRemoteMCPInput(input))
		if err != nil {
			if refusal, ok := registerRemoteMCPToolResult(err); ok {
				return refusal, zero, nil
			}
			return nil, zero, err
		}
		// Bind the registration into the caller's active onboarding workflow —
		// after the mutation, so a refused call leaves no workflow side effects
		// — and record the same registration milestone the catalogue path
		// records. The downstream lifecycle (onboarding status, identity
		// provider attachment, distribution) works off the workflow-bound
		// registration, so without this a remote registration would be
		// invisible to it. An idempotent replay re-binds the same values.
		if onboarding != nil {
			registrationID, err := uuid.Parse(result.Registration)
			if err != nil {
				return nil, zero, ErrUnavailable
			}
			projection, err := onboarding.Start(ctx, principal.OrganizationID, principal.UserID)
			if err != nil {
				return nil, zero, err
			}
			if projection.Workflow == nil {
				return nil, zero, ErrUnavailable
			}
			if _, err := onboarding.BindRegistrationForPrincipal(ctx, principal, result.Project.ID, registrationID); err != nil {
				return nil, zero, err
			}
			if err := onboarding.RecordRegistrationSucceeded(ctx, principal, result.Project.ID, registrationID); err != nil {
				return nil, zero, err
			}
		}
		nextAction := "continue_dashboard_setup"
		message := "The remote MCP server is registered privately for this project. Authentication and headers are configured in the dashboard, never in chat; get_setup_handoff with the returned provider_key and catalog_ref returns this server's Authentication settings page link. Check get_platform_mcp_onboarding_status, complete any required secure dashboard setup, and add the ready server to the project's existing Default plugin."
		if result.BlockedPendingApproval {
			nextAction = "await_org_approval"
			message = "This organization enforces MCP approval and this server is not yet approved. The registration is recorded, but the server stays blocked until an administrator approves it at dashboard_approvals_url."
		}
		return nil, RegisterRemoteMCPToolOutput{
			ProjectSlug:            result.Project.Slug,
			RemoteURL:              result.RemoteURL,
			ReceiptID:              result.Receipt.ID.String(),
			RegistrationID:         result.Registration,
			Replayed:               result.Receipt.Replayed,
			ProviderKey:            remoteURLCatalogProvider,
			CatalogRef:             result.RemoteURL,
			BlockedPendingApproval: result.BlockedPendingApproval,
			DashboardApprovalsURL:  result.DashboardApprovalsURL,
			NextAction:             nextAction,
			Message:                message,
		}, nil
	})
}

// registerUnavailableRegisterRemoteMCPTool declares the same tool with the
// same audiences when the surface's dependencies are not composed, so the tool
// does not appear on and disappear from the endpoint as the rollout flips.
func registerUnavailableRegisterRemoteMCPTool(reg *Registrar) {
	addTool(reg, registerRemoteMCPToolManifest(), ToolMeta{
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit,
	}, unavailableTool(featureRemoteURLRegistration))
}

func registerRemoteMCPToolManifest() *mcp.Tool {
	return &mcp.Tool{
		Meta:         nil,
		Annotations:  nil,
		Description:  "Register a probed remote MCP server as a private source in an explicit AICP project. This tool never accepts a URL: supply the server-issued probe receipt from probe_remote_mcp, and only after the user has explicitly confirmed the probe evidence. Registration creates private project configuration only; it does not distribute the MCP or publish a plugin package, and organization MCP approval enforcement is respected, not bypassed.",
		InputSchema:  nil,
		Name:         "register_remote_mcp_for_project",
		OutputSchema: nil,
		Title:        "Register Remote MCP for Project",
		Icons:        nil,
	}
}

// registerRemoteMCPToolResult maps receipt validation refusals onto their
// bounded result codes, then falls through to the shared operation-budget
// mapping for everything the catalogue registration path already covers.
func registerRemoteMCPToolResult(err error) (*mcp.CallToolResult, bool) {
	var result operationBudgetResult
	switch {
	case errors.Is(err, ErrProbeReceiptExpired):
		result = operationBudgetResult{Code: "receipt_expired", Reason: "", Message: "This probe receipt has expired. Run probe_remote_mcp again, show the fresh evidence to the user for confirmation, and register with the new receipt."}
	case errors.Is(err, ErrProbeReceiptContextMismatch):
		result = operationBudgetResult{Code: "receipt_context_mismatch", Reason: "", Message: "This probe receipt was issued to a different connection. Run probe_remote_mcp from this connection and use the receipt it returns."}
	case errors.Is(err, ErrProbeReceiptInvalid):
		result = operationBudgetResult{Code: "receipt_invalid", Reason: "", Message: "This probe receipt is not valid. Pass the exact probe_receipt value returned by probe_remote_mcp; receipts cannot be constructed or modified."}
	default:
		return operationBudgetToolResult(err)
	}
	return remoteMCPBoundedRefusal(result)
}
