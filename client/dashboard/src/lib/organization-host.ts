/**
 * Moving the dashboard to the active organization's own host.
 *
 * `auth.info` returns `activeOrganizationDashboardUrl` only when the active
 * organization lives on a different configured platform host from the one the
 * request arrived on, so the server owns which hosts qualify. The checks here
 * are the browser's own guard: the target must be an absolute https URL (http only
 * from an http page) on another host, and a tab moves an organization at most once to a given
 * host, so two hosts that disagree can never bounce a tab back and forth.
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
  const params = new URLSearchParams(current.search);
  return (
    current.pathname === "/" &&
    params.get("from_cli") === "true" &&
    Boolean(params.get("cli_callback_url"))
  );
}

function isExempt(current: CurrentLocation): boolean {
  const path = current.pathname.replace(/\/+$/, "");
  return HOST_MOVE_EXEMPT_PATHS.includes(path) || isCliHandoff(current);
}

type CurrentLocation = Pick<
  Location,
  "protocol" | "host" | "pathname" | "search" | "hash"
>;

/**
 * Only https targets qualify. Plain http is allowed only from a page that is
 * itself on http (local development), so a move never downgrades a session.
 */
function allowedProtocol(target: string, current: string): boolean {
  return target === "https:" || (target === "http:" && current === "http:");
}

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
  if (!allowedProtocol(target.protocol, current.protocol)) return undefined;
  if (target.host === current.host) return undefined;

  return target.origin + current.pathname + current.search + current.hash;
}

function moves(): string[] {
  try {
    const parsed = JSON.parse(
      sessionStorage.getItem(MOVED_HOSTS_KEY) ?? "[]",
    ) as unknown;
    return Array.isArray(parsed)
      ? parsed.filter((move): move is string => typeof move === "string")
      : [];
  } catch {
    return [];
  }
}

function moveKey(organizationId: string, target: string): string {
  return `${organizationId} ${new URL(target).host}`;
}

/**
 * Whether this tab already moved this organization from this host to the
 * target's host. The record is per organization, so switching back to another
 * organization that lives on that host still moves the tab.
 */
export function alreadyMovedTo(
  organizationId: string,
  target: string,
): boolean {
  return moves().includes(moveKey(organizationId, target));
}

/**
 * Records the move in this host's tab storage before leaving, so a tab that
 * comes back here with the same organization is not sent off again. Returns
 * false when storage is unavailable: without the guard the move is not safe
 * to make.
 */
export function recordMoveTo(organizationId: string, target: string): boolean {
  try {
    const recorded = moves();
    recorded.push(moveKey(organizationId, target));
    sessionStorage.setItem(MOVED_HOSTS_KEY, JSON.stringify(recorded));
    return true;
  } catch {
    return false;
  }
}
