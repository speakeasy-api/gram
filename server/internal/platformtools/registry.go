package platformtools

import (
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type toolFactory func(telemetry logs.TelemetryService) core.PlatformToolExecutor

var registry = []toolFactory{
	func(telemetrySvc TelemetryService) PlatformToolExecutor {
		return logs.NewSearchLogsTool(telemetrySvc)
	},
}

func BuildExecutors(telemetrySvc TelemetryService) map[string]PlatformToolExecutor {
	executors := make(map[string]PlatformToolExecutor, len(registry))
	for _, factory := range registry {
		executor := factory(telemetrySvc)
		executors[executor.Descriptor().ToolURN().String()] = executor
	}
	return executors
}

func ListPlatformTools() []ToolDescriptor {
	tools := make([]ToolDescriptor, 0, len(registry))
	for _, factory := range registry {
		tools = append(tools, factory(nil).Descriptor())
	}
	return tools
}

func ListTypedTools(projectID uuid.UUID, urnPrefix string) []*types.Tool {
	tools := make([]*types.Tool, 0, len(registry))
	for _, descriptor := range ListPlatformTools() {
		if urnPrefix != "" && !strings.HasPrefix(descriptor.ToolURN().String(), urnPrefix) {
			continue
		}
		tools = append(tools, descriptor.ToTool(projectID))
	}
	return tools
}

func FindToolDescriptor(toolURN urn.Tool) (ToolDescriptor, bool) {
	for _, descriptor := range ListPlatformTools() {
		if descriptor.ToolURN().String() == toolURN.String() {
			return descriptor, true
		}
	}

	var zero ToolDescriptor
	return zero, false
}

func FindToolEntries(toolURNs []string) []*types.ToolEntry {
	entries := make([]*types.ToolEntry, 0, len(toolURNs))
	seen := make(map[string]struct{}, len(toolURNs))
	for _, rawURN := range toolURNs {
		var toolURN urn.Tool
		if err := toolURN.UnmarshalText([]byte(rawURN)); err != nil {
			continue
		}
		descriptor, ok := FindToolDescriptor(toolURN)
		if !ok {
			continue
		}
		if _, ok := seen[descriptor.ToolURN().String()]; ok {
			continue
		}
		seen[descriptor.ToolURN().String()] = struct{}{}
		entries = append(entries, descriptor.ToToolEntry())
	}

	return entries
}

func FindTypedTools(projectID uuid.UUID, toolURNs []string) []*types.Tool {
	tools := make([]*types.Tool, 0, len(toolURNs))
	seen := make(map[string]struct{}, len(toolURNs))
	for _, rawURN := range toolURNs {
		var toolURN urn.Tool
		if err := toolURN.UnmarshalText([]byte(rawURN)); err != nil {
			continue
		}
		descriptor, ok := FindToolDescriptor(toolURN)
		if !ok {
			continue
		}
		if _, ok := seen[descriptor.ToolURN().String()]; ok {
			continue
		}
		seen[descriptor.ToolURN().String()] = struct{}{}
		tools = append(tools, descriptor.ToTool(projectID))
	}

	return tools
}
