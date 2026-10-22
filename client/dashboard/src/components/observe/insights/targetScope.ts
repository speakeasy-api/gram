import type { ObserveDeepLinkScope } from "@/components/observe/observeDeepLink";
import type { ToolUsageTargetSummary } from "@gram/client/models/components/toolusagetargetsummary.js";

export function targetScope(
  target: ToolUsageTargetSummary,
): ObserveDeepLinkScope {
  switch (target.targetType) {
    case "hosted_mcp_server":
      return target.mcpServerId
        ? { target: { type: "mcp_server", id: target.mcpServerId } }
        : { target: { type: "hosted", id: target.targetId } };
    case "meta_mcp_server":
      return { target: { type: "gateway", id: target.targetId } };
    case "shadow_mcp_server":
      return { target: { type: "shadow", id: target.targetId } };
    case "tunneled_mcp_server":
      return { target: { type: "shadow", id: target.targetId } };
    case "skill":
      return { targetTypes: ["skill"] };
    case "local_tool":
    default:
      return { targetTypes: ["local_tool"] };
  }
}
