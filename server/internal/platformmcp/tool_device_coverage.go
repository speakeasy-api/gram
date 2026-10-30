//nolint:exhaustruct // MCP manifests intentionally rely on optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
		Description: "Summarize how much of this organization's fleet runs the Speakeasy device agent, and whether those devices enforce the policy last served to them. The fleet is every device in a connected MDM plus every machine running the agent that no MDM holds (agent_only_devices), so this works with no MDM connected; mdm_connected says whether one is, and without one the gap buckets (no_agent, no_email, unresolved_email, missing) are always 0. agent_active devices are split into policy_attested (applies the served policy), policy_outdated (reports a different or no policy), policy_unreported (no recent report) and enforcement_halted (a blocked build stopped enforcing). attestation is device when every active device matched on its own serial, or user when some only prove their assigned user runs the agent somewhere; agent_active_device_attested counts those matched on their own serial. Narrow to one MDM's inventory with provider.",
		Annotations: readOnlyAnnotations(),
	}, deviceCoverageMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input GetDeviceCoverageInput) (*mcp.CallToolResult, DeviceCoverageOutput, error) {
		return principalToolCall(ctx, deviceCoverageToolResult, func(principal Principal) (DeviceCoverageOutput, error) {
			return service.GetCoverage(ctx, principal, input)
		})
	})
	addTool(reg, &mcp.Tool{
		Name:        operationListManagedDevices,
		Title:       "List Managed Devices",
		Description: "Page through this organization's fleet with each device's coverage and policy enforcement: hostname, serial, user, coverage bucket, policy state, the agent's last report (report_attestation is user when it came from the user's agent, possibly on another machine), and each managed tool's reconcile outcome. The fleet is every device in a connected MDM plus every machine running the agent that no MDM holds; in_inventory is false for the latter, which have no provider, MDM OS or check-in. Filter with coverage_bucket, policy_state (agent_active devices only), user_email, provider or in_inventory, for example policy_state=outdated to find devices not enforcing their policy. Pass next_cursor back as cursor for the next page.",
		Annotations: readOnlyAnnotations(),
	}, deviceCoverageMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListManagedDevicesInput) (*mcp.CallToolResult, ListManagedDevicesOutput, error) {
		return principalToolCall(ctx, deviceCoverageToolResult, func(principal Principal) (ListManagedDevicesOutput, error) {
			return service.ListManagedDevices(ctx, principal, input)
		})
	})
}

// The unavailable tools keep the live input types, so the advertised schema
// does not change with the deployment.
func registerUnavailableDeviceCoverageTools(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        operationGetDeviceCoverage,
		Title:       "Get Device Coverage",
		Description: "Summarize device agent coverage and policy enforcement across the fleet. This is not available on this deployment.",
		Annotations: readOnlyAnnotations(),
	}, deviceCoverageMeta, unavailableDeviceCoverageTool[GetDeviceCoverageInput])
	addTool(reg, &mcp.Tool{
		Name:        operationListManagedDevices,
		Title:       "List Managed Devices",
		Description: "Page through the fleet's devices with their coverage and policy enforcement. This is not available on this deployment.",
		Annotations: readOnlyAnnotations(),
	}, deviceCoverageMeta, unavailableDeviceCoverageTool[ListManagedDevicesInput])
}

func unavailableDeviceCoverageTool[In any](_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, featureUnavailableResult, error) {
	result := featureUnavailableResult{Code: unavailableCode, Feature: deviceCoverageFeature, Message: "Device coverage is not available on this deployment."}
	content, err := json.Marshal(result)
	if err != nil {
		return nil, featureUnavailableResult{}, fmt.Errorf("encode unavailable device coverage: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, result, nil
}

// deviceCoverageToolResult turns the service's sentinels into refusals the
// model can act on. Budget refusals are left to operationBudgetToolResult.
func deviceCoverageToolResult(err error) (*mcp.CallToolResult, bool) {
	result := featureUnavailableResult{Feature: deviceCoverageFeature}
	switch {
	case errors.Is(err, ErrDeviceCoverageInvalid):
		result.Code, result.Message = "invalid_request", "That argument is not one this read accepts. provider is a supported MDM id such as jamf or intune; coverage_bucket is agent_active, agent_stale, agent_other_device, no_agent, no_email, unresolved_email or missing; policy_state is attested, outdated, unreported or halted; limit is 1 to 100; cursor must be a next_cursor from a previous page."
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
