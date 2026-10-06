import { getServerURL } from "@/lib/utils";

import type { DeviceAgentRunMode } from "./device-agent";

/**
 * The address an agent's runtime dials, and the rules for handing it a key.
 * Shared so the wizard and the agent page cannot drift into quoting different
 * URLs for the same agent.
 */

export function agentGatewayURL(agentID: string): string {
  // Absolute, always: an agent runs outside the browser, so a path relative to
  // the dashboard is not an address it can dial. getServerURL can be empty
  // when the build left it unset, in which case the current origin is what is
  // serving MCP.
  const base = getServerURL() || window.location.origin;
  return new URL(`/agent-mcp/${agentID}`, base).toString();
}

/**
 * Whether an endpoint may carry a bearer key. HTTPS everywhere, except
 * loopback, where the request never reaches a network. An unparseable URL is
 * treated as unsafe rather than assumed fine.
 */
export function isCredentialSafe(raw: string): boolean {
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    return false;
  }
  if (parsed.protocol === "https:") return true;
  return (
    parsed.protocol === "http:" &&
    ["localhost", "127.0.0.1", "[::1]", "::1"].includes(parsed.hostname)
  );
}

/**
 * Which script an install code exchanges for. The server checks the key fits
 * it: a device agent code needs a key that can sync and cannot reach MCP.
 */
export type InstallRequest =
  | { flavor: "mcp" }
  | { flavor: "device_agent"; mode: DeviceAgentRunMode };

/**
 * The server's own reason when it gave one: a refusal such as "this key can
 * connect to MCP servers" is the user's to act on, and a bare status is not.
 */
async function installCodeError(response: Response): Promise<string> {
  const fallback = `Could not prepare a setup command (${response.status})`;
  try {
    const body = (await response.json()) as { message?: unknown };
    return typeof body.message === "string" && body.message
      ? body.message
      : fallback;
  } catch {
    return fallback;
  }
}

/**
 * Exchanges a live key for a single-use install code and returns the one-line
 * setup command. The key never goes into the command that way: a one-liner
 * ends up in shell history and pasted into threads, and a code that dies on
 * first use is worthless there.
 */
export async function mintInstallCommand(
  gatewayURL: string,
  secret: string,
  request: InstallRequest = { flavor: "mcp" },
): Promise<string> {
  const response = await fetch(`${gatewayURL}/install-code`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${secret}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    throw new Error(await installCodeError(response));
  }
  const { code } = (await response.json()) as { code: string };
  // This value is pasted into a shell. It is minted by our own server, but a
  // command built by interpolation is only safe if what goes into it cannot
  // carry shell syntax, so that is checked here rather than assumed upstream.
  if (!/^[A-Za-z0-9_-]+$/.test(code)) {
    throw new Error("Could not prepare a setup command (malformed code)");
  }
  const base = new URL(gatewayURL);
  return `curl -fsSL ${base.origin}/agent-mcp/install/${code} | sh`;
}
