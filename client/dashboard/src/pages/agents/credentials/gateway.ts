import { useCustomDomain } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import type { CustomDomain } from "@gram/client/models/components/customdomain.js";

/**
 * The address an agent's runtime dials, and the rules for handing it a key.
 * Shared so the wizard and the agent page cannot drift into quoting different
 * URLs for the same agent.
 */

export function agentGatewayURL(agentID: string, base?: string): string {
  // Absolute, always: an agent runs outside the browser, so a path relative to
  // the dashboard is not an address it can dial. getServerURL can be empty
  // when the build left it unset, in which case the current origin is what is
  // serving MCP.
  return new URL(`/agent-mcp/${agentID}`, base ?? platformBase()).toString();
}

function platformBase(): string {
  return getServerURL() || window.location.origin;
}

/**
 * The origin agents must dial when it is not the platform host. An
 * organization whose custom domain has an IP allowlist is locked out of the
 * platform host, so its agents reach the gateway on that domain, where the
 * allowlist is enforced. Every other organization uses the platform host.
 */
export function agentGatewayBase(
  domain: Pick<CustomDomain, "domain" | "ipAllowlist"> | undefined,
): string | undefined {
  if (!domain || domain.ipAllowlist.length === 0) return undefined;
  return `https://${domain.domain}`;
}

/** agentGatewayURL on the organization's own origin. Empty without an agent. */
export function useAgentGatewayURL(agentID: string | null | undefined): string {
  const { domain } = useCustomDomain();
  if (!agentID) return "";
  return agentGatewayURL(agentID, agentGatewayBase(domain));
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
 * Exchanges a live key for a short-lived install code and returns the one-line
 * setup command. The key never goes into the command that way: a one-liner
 * ends up in shell history and pasted into threads. The code can be redeemed
 * any number of times until it expires 15 minutes after it is minted, so a
 * leaked command can retrieve the key until then.
 */
export async function mintInstallCommand(
  gatewayURL: string,
  secret: string,
): Promise<string> {
  // Minted on the platform host whatever origin the gateway is quoted on: the
  // browser may not be inside a custom domain's IP allowlist, and the server
  // writes the organization's own gateway address into the installed config.
  const installURL = new URL(new URL(gatewayURL).pathname, platformBase());
  const response = await fetch(`${installURL.toString()}/install-code`, {
    method: "POST",
    headers: { Authorization: `Bearer ${secret}` },
  });
  if (!response.ok) {
    throw new Error(`Could not prepare a setup command (${response.status})`);
  }
  const { code } = (await response.json()) as { code: string };
  // This value is pasted into a shell. It is minted by our own server, but a
  // command built by interpolation is only safe if what goes into it cannot
  // carry shell syntax, so that is checked here rather than assumed upstream.
  if (!/^[A-Za-z0-9_-]+$/.test(code)) {
    throw new Error("Could not prepare a setup command (malformed code)");
  }
  return `curl -fsSL ${installURL.origin}/agent-mcp/install/${code} | sh`;
}
