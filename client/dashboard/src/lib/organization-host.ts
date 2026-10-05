/**
 * Moving the dashboard to the active organization's own host.
 *
 * `auth.info` returns `activeOrganizationDashboardUrl` only when the active
 * organization lives on a different configured platform host from the one the
 * request arrived on, so the server owns which hosts qualify. The checks here
 * are the browser's own guard: the target must be an absolute http(s) URL on
 * another host, and a tab moves at most once to a given host, so two hosts
 * that disagree can never bounce a tab back and forth.
 */

const MOVED_HOSTS_KEY = "organizationHostMoves";

/**
 * Pages that finish a hand-off and stay on the host they were opened on. Each
 * takes a bearer token from the URL fragment into this host's session storage,
 * which another host cannot read, so moving mid-flow would lose the token.
 */
const HOST_MOVE_EXEMPT_PATHS = [
  "/shadow-mcp/request",
  "/risk-policy-bypass/request",
  "/risk-policy-challenge/acknowledge",
];

/**
 * The CLI login hand-off ("/?from_cli=true&cli_callback_url=…") returns a key
 * to a local callback and must finish on the host the CLI opened.
 */
function isCliHandoff(current: CurrentLocation): boolean {
  return new URLSearchParams(current.search).get("from_cli") === "true";
}

function isExempt(current: CurrentLocation): boolean {
  const path = current.pathname.replace(/\/+$/, "");
  return HOST_MOVE_EXEMPT_PATHS.includes(path) || isCliHandoff(current);
}

type CurrentLocation = Pick<Location, "host" | "pathname" | "search" | "hash">;

/**
 * The URL that keeps the current path, query and hash on the organization's
 * host, or undefined when the dashboard should stay where it is.
 */
export function organizationHostRedirectTarget(
  dashboardUrl: string | undefined,
  current: CurrentLocation,
): string | undefined {
  if (!dashboardUrl || isExempt(current)) return undefined;

  let target: URL;
  try {
    target = new URL(dashboardUrl);
  } catch {
    return undefined;
  }
  if (target.protocol !== "https:" && target.protocol !== "http:") {
    return undefined;
  }
  if (target.host === current.host) return undefined;

  return target.origin + current.pathname + current.search + current.hash;
}

function movedHosts(): string[] {
  try {
    const parsed = JSON.parse(
      sessionStorage.getItem(MOVED_HOSTS_KEY) ?? "[]",
    ) as unknown;
    return Array.isArray(parsed)
      ? parsed.filter((host): host is string => typeof host === "string")
      : [];
  } catch {
    return [];
  }
}

/** Whether this tab already moved from this host to the target's host. */
export function alreadyMovedTo(target: string): boolean {
  return movedHosts().includes(new URL(target).host);
}

/**
 * Records the move in this host's tab storage before leaving, so a tab that
 * comes back here is not sent off again. Returns false when storage is
 * unavailable: without the guard the move is not safe to make.
 */
export function recordMoveTo(target: string): boolean {
  try {
    const hosts = movedHosts();
    hosts.push(new URL(target).host);
    sessionStorage.setItem(MOVED_HOSTS_KEY, JSON.stringify(hosts));
    return true;
  } catch {
    return false;
  }
}
