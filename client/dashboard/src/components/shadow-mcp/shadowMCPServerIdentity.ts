import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { formatPlatform } from "@/lib/formatPlatform";

/**
 * The synthetic identity an LLM proxy row carries when the proxy saw an MCP
 * server only by the <server> segment of its namespaced tool names
 * (mcp__<server>__<tool>). It passes through every URL-keyed layer of the
 * inventory, but it is a key, not an address — never present it as a URL.
 */
const TOOL_NAMESPACE_SCHEME = "mcp-tool://";

type ShadowMCPTargetKind = ShadowMCPInventoryServer["targetKind"];

export function isToolNamespaceServer(
  server: Pick<ShadowMCPInventoryServer, "targetKind">,
): boolean {
  return server.targetKind === "tool_namespace";
}

/**
 * Whether a review decision on this target is recorded without enforcement:
 * a local command has no URL for grants to attach to, and a tool namespace
 * has no resolved URL yet. Both read as a dash in the Status column and word
 * the decide sheet as a decision of record.
 */
export function isObserveOnlyShadowMCPTarget(
  targetKind: ShadowMCPTargetKind,
): boolean {
  return targetKind === "stdio_command" || targetKind === "tool_namespace";
}

/** "mcp-tool://github" -> "github"; a non-namespace value passes through. */
function toolNamespaceServerName(canonicalServerUrl: string): string {
  if (!canonicalServerUrl.startsWith(TOOL_NAMESPACE_SCHEME)) {
    return canonicalServerUrl;
  }
  return canonicalServerUrl.slice(TOOL_NAMESPACE_SCHEME.length);
}

/**
 * The name a row goes by: the custom or reported server name, else the URL
 * host, else — for a tool namespace, which has no host — the namespace
 * itself rather than the synthetic mcp-tool:// key.
 */
export function shadowMCPInventoryServerLabel(
  server: Pick<
    ShadowMCPInventoryServer,
    "canonicalServerUrl" | "serverName" | "targetKind" | "urlHost"
  >,
): string {
  if (server.serverName || server.urlHost) {
    return server.serverName || server.urlHost;
  }
  return isToolNamespaceServer(server)
    ? toolNamespaceServerName(server.canonicalServerUrl)
    : server.canonicalServerUrl;
}

/** The tool-name pattern the proxy observed: "mcp-tool://github" -> "mcp__github__*". */
export function toolNamespacePattern(canonicalServerUrl: string): string {
  return `mcp__${toolNamespaceServerName(canonicalServerUrl)}__*`;
}

export const TOOL_NAMESPACE_UNRESOLVED_HINT =
  "Seen only by the LLM proxy as a tool namespace. Configure the LiteLLM MCP gateway to resolve the server URL.";

/**
 * Display labels for the hook sources that observed a server, in the order
 * the server reported them (sorted, de-duplicated). Aliases collapse to one
 * label, so the list is de-duplicated again after formatting.
 */
export function shadowMCPSourceLabels(sources: string[] | undefined): string[] {
  const labels: string[] = [];
  for (const source of sources ?? []) {
    const label = formatPlatform(source);
    if (label.length > 0 && !labels.includes(label)) labels.push(label);
  }
  return labels;
}
