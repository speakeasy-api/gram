//nolint:exhaustruct // MCP manifests intentionally rely on optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	operationGetDeviceCoverage  = "get_device_coverage"
	operationListManagedDevices = "list_managed_devices"
)

var deviceCoverageMeta = ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeNone, DiscoveryScopes: discoveryOrgRead}

func registerDeviceCoverageTools(reg *Registrar, service *DeviceCoverageService) {
	if !service.valid() {
		registerUnavailableDeviceCoverageTools(reg)
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        operationGetDeviceCoverage,
		Title:       "Get Device Coverage",
		Description: "Summarize how much of this organization's MDM-managed fleet runs the Speakeasy device agent, and whether those devices enforce the policy last served to them. agent_active devices are split into policy_attested (applies the served policy), policy_outdated (reports a different or no policy), policy_unreported (no recent report) and enforcement_halted (a blocked build stopped enforcing). attestation is device when every active device matched on its own serial, or user when some only prove their assigned user runs the agent somewhere. Narrow to one MDM with provider.",
		Annotations: readOnlyAnnotations(),
	}, deviceCoverageMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input GetDeviceCoverageInput) (*mcp.CallToolResult, DeviceCoverageOutput, error) {
		return principalToolCall(ctx, deviceCoverageToolResult, func(principal Principal) (DeviceCoverageOutput, error) {
			return service.GetCoverage(ctx, principal, input)
		})
	})
	addTool(reg, &mcp.Tool{
		Name:        operationListManagedDevices,
		Title:       "List Managed Devices",
		Description: "Page through this organization's MDM-managed devices with each one's coverage and policy enforcement: hostname, serial, assigned user, coverage bucket, policy state, the agent's last report, and each managed tool's reconcile outcome. Filter with coverage_bucket, policy_state (agent_active devices only), user_email or provider, for example policy_state=outdated to find devices not enforcing their policy. Pass next_cursor back as cursor for the next page.",
		Annotations: readOnlyAnnotations(),
	}, deviceCoverageMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListManagedDevicesInput) (*mcp.CallToolResult, ListManagedDevicesOutput, error) {
		return principalToolCall(ctx, deviceCoverageToolResult, func(principal Principal) (ListManagedDevicesOutput, error) {
			return service.ListManagedDevices(ctx, principal, input)
		})
	})
}

func registerUnavailableDeviceCoverageTools(reg *Registrar) {
	for _, tool := range []struct{ name, title, description string }{
		{operationGetDeviceCoverage, "Get Device Coverage", "Summarize device agent coverage and policy enforcement across the MDM fleet. This is not available on this deployment."},
		{operationListManagedDevices, "List Managed Devices", "Page through MDM-managed devices with their coverage and policy enforcement. This is not available on this deployment."},
	} {
		addTool(reg, &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description, Annotations: readOnlyAnnotations()}, deviceCoverageMeta, unavailableTool(deviceCoverageFeature))
	}
}

// deviceCoverageToolResult turns the service's sentinels into refusals the
// model can act on. Budget refusals are left to operationBudgetToolResult.
func deviceCoverageToolResult(err error) (*mcp.CallToolResult, bool) {
	result := featureUnavailableResult{Feature: deviceCoverageFeature}
	switch {
	case errors.Is(err, ErrDeviceCoverageInvalid):
		result.Code, result.Message = "invalid_request", "That argument is not one this read accepts. coverage_bucket is agent_active, agent_stale, agent_other_device, no_agent, no_email, unresolved_email or missing; policy_state is attested, outdated, unreported or halted; limit is 1 to 100; cursor must be a next_cursor from a previous page."
	case errors.Is(err, ErrDeviceCoverageUnavailable):
		result.Code, result.Message = unavailableCode, "Device coverage is temporarily unavailable."
	default:
		return operationBudgetToolResult(err)
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
