import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import type { ServerTool } from "./serverMerge";

/**
 * Maps stored remote-MCP tool metadata rows into the `ServerTool` shape the
 * grant "Specific tools" picker renders.
 *
 * Remote/tunneled servers resolve their tools at call time, so they carry no
 * enumerable tool list at deploy time (serverMerge gives them `tools: []`).
 * The Inspect tab materializes what the live server advertises into the tool
 * metadata table; this is how the role editor turns that table back into
 * selectable rows. The stored annotation hints become the same disposition-hint
 * object toolset tools carry, so annotation counting and selection treat remote
 * and toolset tools uniformly.
 */
export function toolMetadataToServerTools(
  serverId: string,
  metadata: ToolMetadata[],
): ServerTool[] {
  return metadata.map((m) => ({
    // Tool rows only need a stable React key and a name to match selectors on;
    // the metadata table has no per-tool id, so synthesize one from the grant
    // resource id and the tool name (unique within a server).
    id: `${serverId}:${m.toolName}`,
    name: m.toolName,
    type: "remotemcp",
    annotations: {
      readOnlyHint: m.readOnlyHint,
      destructiveHint: m.destructiveHint,
      idempotentHint: m.idempotentHint,
      openWorldHint: m.openWorldHint,
    },
  }));
}
