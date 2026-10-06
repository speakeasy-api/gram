/**
 * Moving the dashboard to the active organization's own host.
 *
 * `auth.info` returns `activeOrganizationDashboardUrl` only when the active
 * organization lives on a different configured platform host from the one the
 * request arrived on, so the server owns which hosts qualify. The move hands
 * the session over with a session transfer (`auth.transferIn` start mode on
 * the other host), so the user does not sign in again.
 *
 * A transfer starts only when all of these hold:
 * - the organization's default_host is non-NULL (NULL is the legacy host and
 *   never gets a dashboard URL, so it never moves),
 * - that host differs from the current host,
 * - the current path is not exempt (below),
 * - and this tab has not already recorded a move of this organization to that
 *   host in sessionStorage.
 *
 * The move is recorded before the browser leaves, so at most one automatic
 * transfer per organization and host happens per tab. Any failure along the
 * way (an expired code, a missing or overwritten nonce cookie, a session
 * store error, or an organization host that is not a configured platform
 * host) ends on the other host's login page, or leaves the tab here, and
 * never starts a second automatic transfer. The checks here
 * are the browser's own guard: the target must be an absolute https URL (http
 * only from an http page) on another host, and a tab moves an organization at
 * most once to a given host, so two hosts that disagree can never bounce a tab
 * back and forth.
 */

import { isCliAuthFlowLocation } from "@/lib/cli-auth-flow";

const HOST_MOVES_KEY = "organizationHostMoves";

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

function isExempt(current: CurrentLocation): boolean {
  const path = current.pathname.replace(/\/+$/, "");
  // The CLI login hand-off must finish on the host the CLI opened.
  return (
    HOST_MOVE_EXEMPT_PATHS.includes(path) ||
    isCliAuthFlowLocation(current.pathname, current.search)
  );
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
 * The URL that starts a session transfer to the organization's host and lands
 * on the current path, query and hash there, or undefined when the dashboard
 * should stay where it is.
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

  const params = new URLSearchParams({
    source_host: current.host,
    redirect: current.pathname + current.search + current.hash,
  });
  return `${target.origin}/rpc/auth.transferIn?${params.toString()}`;
}

function moves(): string[] {
  try {
    const parsed = JSON.parse(
      sessionStorage.getItem(HOST_MOVES_KEY) ?? "[]",
    ) as unknown;
    return Array.isArray(parsed)
      ? parsed.filter((move): move is string => typeof move === "string")
      : [];
  } catch {
    return [];
  }
}

/**
 * Identifies moving organizationId to target's host. Moves are recorded per
 * organization, so switching back to another organization that lives on a
 * host this tab already visited still moves the tab.
 */
export function moveKey(organizationId: string, target: string): string {
  return `${organizationId} ${new URL(target).host}`;
}

/** Whether this tab already made the move from this host. */
export function alreadyMoved(key: string): boolean {
  return moves().includes(key);
}

/**
 * Records the move in this host's tab storage before leaving, so a tab that
 * comes back here with the same organization is not sent off again. Returns
 * false when storage is unavailable: without the guard the move is not safe
 * to make.
 */
export function recordMove(key: string): boolean {
  try {
    sessionStorage.setItem(HOST_MOVES_KEY, JSON.stringify([...moves(), key]));
    return true;
  } catch {
    return false;
  }
}
