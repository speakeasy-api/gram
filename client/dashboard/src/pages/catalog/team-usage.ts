import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

/**
 * The query param that switches the catalog to the servers the team already
 * connects to directly. It lives in the URL so the view survives a reload.
 */
export const CATALOG_SHOW_PARAM = "show";
export const CATALOG_SHOW_TEAM = "team";
export type CatalogShow = "all" | typeof CATALOG_SHOW_TEAM;

/**
 * Hosts Speakeasy can never reach from its own infrastructure: the machine
 * the agent runs on, and the private networks behind it. A server seen at one
 * of these is local to someone's laptop or office, so it cannot be added as a
 * hosted MCP server.
 */
const LOCAL_HOSTNAMES = new Set(["localhost", "0.0.0.0", "[::]", "[::1]"]);
const LOCAL_SUFFIXES = [".localhost", ".local", ".internal", ".lan", ".home"];

function isPrivateIPv4(hostname: string): boolean {
  const parts = hostname.split(".");
  if (parts.length !== 4) return false;
  const octets = parts.map((part) => Number(part));
  if (octets.some((octet) => !Number.isInteger(octet) || octet < 0)) {
    return false;
  }
  const [a, b] = octets as [number, number, number, number];
  if (a === 0 || a === 10 || a === 127) return true;
  if (a === 169 && b === 254) return true;
  if (a === 172 && b >= 16 && b <= 31) return true;
  if (a === 192 && b === 168) return true;
  if (a === 100 && b >= 64 && b <= 127) return true;
  return false;
}

/**
 * The IPv4 address an IPv4-mapped IPv6 address carries (`::ffff:a.b.c.d`).
 * The URL parser rewrites the dotted tail as two hex groups
 * (`::ffff:7f00:1`), so both spellings are decoded.
 */
function mappedIPv4(address: string): string | null {
  const match = /^(?:0{0,4}:){0,5}:?ffff:(.+)$/.exec(address);
  if (!match?.[1]) return null;
  const tail = match[1];
  if (tail.includes(".")) return tail;
  const groups = tail.split(":");
  if (groups.length !== 2) return null;
  const [high, low] = groups.map((group) => Number.parseInt(group, 16));
  if (high === undefined || low === undefined) return null;
  if (Number.isNaN(high) || Number.isNaN(low)) return null;
  return [high >> 8, high & 0xff, low >> 8, low & 0xff].join(".");
}

function isPrivateIPv6(hostname: string): boolean {
  if (!hostname.startsWith("[")) return false;
  const address = hostname.slice(1, -1).toLowerCase();
  const mapped = mappedIPv4(address);
  if (mapped) return isPrivateIPv4(mapped);
  return (
    address.startsWith("fc") ||
    address.startsWith("fd") ||
    address.startsWith("fe8") ||
    address.startsWith("fe9") ||
    address.startsWith("fea") ||
    address.startsWith("feb")
  );
}

/** Whether a URL points at the agent's own machine or a private network. */
export function isLocalServerUrl(raw: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(raw.trim());
  } catch {
    return true;
  }
  const hostname = parsed.hostname.toLowerCase();
  if (LOCAL_HOSTNAMES.has(hostname)) return true;
  if (LOCAL_SUFFIXES.some((suffix) => hostname.endsWith(suffix))) return true;
  // A bare name with no dot (`http://mcp:8080`) only resolves on a local
  // network or inside a container.
  if (!hostname.includes(".") && !hostname.startsWith("[")) return true;
  return isPrivateIPv4(hostname) || isPrivateIPv6(hostname);
}

/**
 * Whether Speakeasy can host this Shadow MCP server: it must be reached over
 * HTTP at a public address. Local commands and servers on the agent's own
 * machine or network are left out, since nobody else could connect to them.
 */
export function isHostableShadowServer(
  server: Pick<ShadowMCPInventoryServer, "targetKind" | "canonicalServerUrl">,
): boolean {
  if (server.targetKind === "stdio_command") return false;
  let parsed: URL;
  try {
    parsed = new URL(server.canonicalServerUrl.trim());
  } catch {
    return false;
  }
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return false;
  return !isLocalServerUrl(server.canonicalServerUrl);
}

/** The name people know a Shadow MCP server by. */
export function teamServerLabel(
  server: Pick<
    ShadowMCPInventoryServer,
    "serverName" | "urlHost" | "canonicalServerUrl"
  >,
): string {
  return server.serverName || server.urlHost || server.canonicalServerUrl;
}

/**
 * The servers the team connects to directly that Speakeasy could host, most
 * used first. Ties go to the server more people use, then to the name, so the
 * order is stable between refreshes.
 */
export function teamUsedServers(
  servers: readonly ShadowMCPInventoryServer[],
): ShadowMCPInventoryServer[] {
  return servers
    .filter(isHostableShadowServer)
    .sort(
      (a, b) =>
        b.observedUseCount - a.observedUseCount ||
        b.userCount - a.userCount ||
        teamServerLabel(a).localeCompare(teamServerLabel(b)),
    );
}

/** The project server that already serves this URL, if one does. */
export function teamServerExistingServer(
  server: Pick<
    ShadowMCPInventoryServer,
    "officialServer" | "suggestedOfficialServer"
  >,
): { mcpServerId: string; slug?: string; name: string } | null {
  return server.officialServer ?? server.suggestedOfficialServer ?? null;
}

/** Query params the remote MCP create page reads to start prefilled. */
export const PREFILL_URL_PARAM = "url";
export const PREFILL_NAME_PARAM = "name";

/** The remote MCP create page, with this server's URL and name filled in. */
export function remoteMcpPrefillHref(
  createHref: string,
  server: Pick<
    ShadowMCPInventoryServer,
    "canonicalServerUrl" | "serverName" | "urlHost"
  >,
): string {
  const params = new URLSearchParams({
    [PREFILL_URL_PARAM]: server.canonicalServerUrl,
  });
  const name = server.serverName || server.urlHost;
  if (name) params.set(PREFILL_NAME_PARAM, name);
  return `${createHref}?${params.toString()}`;
}
