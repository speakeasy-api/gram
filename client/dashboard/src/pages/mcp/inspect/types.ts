import type {
  ProxiedMcpToolAnnotations,
  ProxiedMcpToolUnavailableReason,
} from "@/hooks/useProxiedMcpTools";
import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import type { McpToolRun } from "./runMcpTool";

/** One tool as Inspect shows it, whatever kind of server it came from. */
export interface InspectTool {
  /** The name an agent calls it by. */
  name: string;
  /** A shorter name for the list, when the group already says the rest. */
  label?: string;
  description?: string;
  inputSchema?: unknown;
  annotations?: ProxiedMcpToolAnnotations;
  /** Speakeasy's stored annotations, for servers that keep them. */
  stored?: ToolMetadata;
  /** Set when the server lists the tool but this viewer can't use it. */
  unavailable?: { reason?: ProxiedMcpToolUnavailableReason };
  /** A heading to group the tool under, e.g. a gateway member's name. */
  group?: string;
  /** The server a gateway member's tool comes from, e.g. "Linear". */
  source?: string;
}

/**
 * Speakeasy's stored annotation wins over what the server declares. A
 * read-only tool is never destructive, whatever its destructive hint says.
 */
export function isDestructive(tool: InspectTool): boolean {
  const readOnly = tool.stored?.readOnlyHint ?? tool.annotations?.readOnlyHint;
  if (readOnly === true) return false;
  return (
    (tool.stored?.destructiveHint ?? tool.annotations?.destructiveHint) === true
  );
}

/** User-facing text for why a tool is unavailable. */
export function unavailableReasonLabel(
  reason: ProxiedMcpToolUnavailableReason | undefined,
): string {
  switch (reason) {
    case "blocked":
      return "Blocked by a role rule";
    case "read_only":
      return "Your access is read-only";
    case "not_granted":
      return "Not granted to you";
    case undefined:
      return "Not available to you";
  }
}

/**
 * What the run panel keeps for one tool while the user moves around Inspect:
 * the arguments typed so far and the last run's outcome.
 */
export interface ToolDraft {
  values: Record<string, string>;
  result?: McpToolRun;
}

export const EMPTY_DRAFT: ToolDraft = { values: {} };
