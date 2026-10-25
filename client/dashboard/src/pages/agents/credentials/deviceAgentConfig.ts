/**
 * The managed config that points a headless machine's device agent at one
 * agent identity. The device agent reads it, then writes an MCP entry for
 * each AI tool that fetches the key from the device agent at run time, so the
 * key lives here and nowhere in the tools' own config files.
 */

// The control plane the device agent dials when the config names none.
const DEFAULT_CONTROL_PLANE = "https://app.getgram.ai";

// TODO(device-agent): the managed.json field names for an agent identity key
// are not final in the device agent yet. This function is the only place they
// are spelled; rename them here when that lands.
export function deviceAgentManagedConfig({
  agentKey,
  agentID,
  serverOrigin,
}: {
  agentKey: string;
  agentID: string;
  /** Origin of the server this dashboard talks to. */
  serverOrigin: string;
}): string {
  const config: Record<string, string | number> = {
    v: 1,
    agent_key: agentKey,
    agent_id: agentID,
  };
  // Only a deployment other than the default one (local development, a
  // self-hosted install) needs to say where the device agent should connect.
  if (serverOrigin !== "" && serverOrigin !== DEFAULT_CONTROL_PLANE) {
    config._control_plane_url = serverOrigin;
  }
  return JSON.stringify(config, null, 2);
}
