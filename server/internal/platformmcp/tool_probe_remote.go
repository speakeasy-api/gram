package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// featureRemoteURLRegistration names the remote URL source registration
// surface in bounded feature_unavailable refusals, for the stub counterparts
// and for organizations outside the surface rollout alike.
const featureRemoteURLRegistration = "remote_url_registration"

// RemoteMCPProber verifies one user-supplied remote MCP URL and, on
// verification, issues the signed probe receipt the mutating registration tool
// accepts in place of a raw URL. *RemoteProbeService is the production
// implementation.
type RemoteMCPProber interface {
	Probe(ctx context.Context, principal Principal, remoteURL string) (RemoteProbeResult, error)
}

type ProbeRemoteMCPToolInput struct {
	RemoteURL string `json:"remote_url" jsonschema:"https URL of the remote MCP server to verify; userinfo and fragments are refused; the probe is read-only and never registers anything"`
}

type ProbeRemoteMCPToolOutput struct {
	// Evidence is what the probe observed. It must be shown to the user —
	// including its gaps — for explicit confirmation before registering.
	Evidence ProbeEvidence `json:"evidence"`

	// ProbeReceipt is the server-issued identity register_remote_mcp_for_project
	// accepts. It is bound to this connection and to the evidence above.
	ProbeReceipt string `json:"probe_receipt"`

	// ReceiptExpiresAt is when the receipt stops being redeemable, RFC 3339.
	ReceiptExpiresAt string `json:"receipt_expires_at"`

	NextAction string `json:"next_action"`
	Message    string `json:"message"`
}

func registerProbeRemoteMCPTool(reg *Registrar, prober RemoteMCPProber, surfaceGate Gate) {
	addTool(reg, probeRemoteMCPToolManifest(), ToolMeta{
		// External-only: probe receipts bind to the probing connection, which a
		// connection-less surface cannot present at registration time.
		Audiences: externalOnly, ProjectScope: ProjectScopeNone,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProbeRemoteMCPToolInput) (*mcp.CallToolResult, ProbeRemoteMCPToolOutput, error) {
		var zero ProbeRemoteMCPToolOutput
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, zero, err
		}
		if refusal, ok := remoteMCPSurfaceRefusal(ctx, surfaceGate, principal); ok {
			return refusal, zero, nil
		}
		result, err := prober.Probe(ctx, principal, input.RemoteURL)
		if err != nil {
			if refusal, ok := probeRemoteMCPToolResult(err); ok {
				return refusal, zero, nil
			}
			return nil, zero, fmt.Errorf("probe remote mcp: %w", err)
		}
		return nil, ProbeRemoteMCPToolOutput{
			Evidence:         result.Evidence,
			ProbeReceipt:     result.Receipt,
			ReceiptExpiresAt: result.ReceiptExpiresAt.UTC().Format(time.RFC3339),
			NextAction:       "confirm_evidence_with_user",
			Message:          "The URL verified as an MCP server. Show this evidence to the user — including every gap — and get their explicit confirmation before calling register_remote_mcp_for_project with the probe receipt. The receipt expires at receipt_expires_at; after that, re-probe.",
		}, nil
	})
}

// registerUnavailableProbeRemoteMCPTool declares the same tool with the same
// audiences when the surface's dependencies are not composed, so the tool does
// not appear on and disappear from the endpoint as the rollout flips.
func registerUnavailableProbeRemoteMCPTool(reg *Registrar) {
	addTool(reg, probeRemoteMCPToolManifest(), ToolMeta{
		Audiences: externalOnly, ProjectScope: ProjectScopeNone,
	}, unavailableTool(featureRemoteURLRegistration))
}

func probeRemoteMCPToolManifest() *mcp.Tool {
	return &mcp.Tool{
		Meta:         nil,
		Annotations:  readOnlyAnnotations(),
		Description:  "Verify that a user-supplied https URL hosts a real remote MCP server. This is the only tool that accepts a URL from chat; it is read-only and registers nothing. On verification it returns bounded evidence to show the user plus a short-lived signed probe receipt, which register_remote_mcp_for_project accepts in place of the URL after the user explicitly confirms the evidence.",
		InputSchema:  nil,
		Name:         "probe_remote_mcp",
		OutputSchema: nil,
		Title:        "Probe Remote MCP",
		Icons:        nil,
	}
}

// remoteMCPSurfaceRefusal enforces the remote URL surface rollout gate for one
// call. An evaluation failure reads exactly like a disabled organization: the
// surface fails closed and the refusal carries no gate or flag detail.
func remoteMCPSurfaceRefusal(ctx context.Context, surfaceGate Gate, principal Principal) (*mcp.CallToolResult, bool) {
	enabled, err := surfaceGate.Enabled(ctx, principal.OrganizationID)
	if err != nil || !enabled {
		return remoteMCPBoundedRefusal(featureUnavailableResult{
			Code:    unavailableCode,
			Feature: featureRemoteURLRegistration,
			Message: "Remote MCP URL registration is not enabled for this organization. Reviewed catalogue registration may still be available through search_mcp_catalog and register_catalog_mcp.",
		})
	}
	return nil, false
}

// probeRemoteMCPToolResult maps the probe service's typed refusals onto the
// bounded result codes the design table names. Anything unrecognized falls
// through to the shared operation-budget mapping, and past that surfaces as a
// transport error.
func probeRemoteMCPToolResult(err error) (*mcp.CallToolResult, bool) {
	var result operationBudgetResult
	switch {
	case errors.Is(err, ErrRemoteURLInvalid):
		result = operationBudgetResult{Code: "invalid_url", Reason: "", Message: "This URL fails the shape rules: it must be https, with no userinfo and no fragment. Correct the URL with the user before probing again; nothing was contacted."}
	case errors.Is(err, ErrProbeEgressDenied):
		result = operationBudgetResult{Code: "egress_denied", Reason: "", Message: "The egress policy does not allow probing this target. Private and internal addresses cannot be registered as remote MCP sources."}
	case errors.Is(err, ErrProbeUnreachable):
		result = operationBudgetResult{Code: "unreachable", Reason: "", Message: "The target could not be reached within the probe's bounded time. Confirm the URL with the user and retry once the server is reachable."}
	case errors.Is(err, ErrProbeNotMCPServer):
		result = operationBudgetResult{Code: "not_an_mcp_server", Reason: "", Message: "The target answered but did not behave like an MCP server: no completed initialize handshake and no typed auth rejection. Confirm the exact MCP endpoint URL with the user."}
	case errors.Is(err, ErrProbeReceiptInvalid):
		result = operationBudgetResult{Code: "receipt_invalid", Reason: "", Message: "A probe receipt cannot be issued to this caller identity, so the probe was not run. Re-authenticate the Platform MCP connection and retry."}
	default:
		return operationBudgetToolResult(err)
	}
	return remoteMCPBoundedRefusal(result)
}

// remoteMCPBoundedRefusal renders one bounded refusal payload as an MCP error
// result. A payload that cannot be encoded is not a refusal the model should
// act on, so the caller falls back to a transport error.
func remoteMCPBoundedRefusal(payload any) (*mcp.CallToolResult, bool) {
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return &mcp.CallToolResult{
		Meta:              nil,
		Content:           []mcp.Content{&mcp.TextContent{Text: string(content), Meta: nil, Annotations: nil}},
		StructuredContent: nil,
		IsError:           true,
	}, true
}
