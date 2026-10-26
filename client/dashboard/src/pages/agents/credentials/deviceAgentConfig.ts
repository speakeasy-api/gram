/**
 * The managed config that points a headless machine's device agent at one
 * agent identity. The key goes in as `mcp_credential`: the device agent uses
 * it for MCP only (no plugin sync, hooks or telemetry) and writes an MCP entry
 * for each AI tool that sends the key as a static bearer header.
 */

// The control plane the device agent dials when the config names none.
const DEFAULT_CONTROL_PLANE = "https://app.getgram.ai";

// The only place the managed.json field names for an agent identity's MCP-only
// key are spelled; they match the device agent's docs/MANAGED_CONFIG.md.
export function deviceAgentManagedConfig({
  mcpCredential,
  agentID,
  serverOrigin,
}: {
  mcpCredential: string;
  agentID: string;
  /** Origin of the server this dashboard talks to. */
  serverOrigin: string;
}): string {
  const config: Record<string, string | number> = {
    v: 1,
    mcp_credential: mcpCredential,
    agent_id: agentID,
  };
  // Only a deployment other than the default one (local development, a
  // self-hosted install) needs to say where the device agent should connect.
  if (serverOrigin !== "" && serverOrigin !== DEFAULT_CONTROL_PLANE) {
    config._control_plane_url = serverOrigin;
  }
  return JSON.stringify(config, null, 2);
}
