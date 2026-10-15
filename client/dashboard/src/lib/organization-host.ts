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
 * - and this tab's sessionStorage guard allows it: no move of this
 *   organization to that host in the last HOST_MOVE_WINDOW_MS, and fewer than
 *   HOST_MOVE_LIMIT moves of it in the last HOST_MOVE_LIMIT_WINDOW_MS.
 *
 * The move is recorded before the browser leaves, so a tab that comes straight
 * back makes no second automatic transfer. Any failure along the way (an
 * expired code, a missing or overwritten nonce cookie, a session store error,
 * or an organization host that is not a configured platform host) ends on the
 * other host's login page, or leaves the tab here, and never starts a second
 * automatic transfer. The checks here are the browser's own guard: the target
 * must be an absolute https URL (http only from an http page) on another host.
 * The short window stops a fast bounce between two hosts that disagree, and the
 * count stops a slow one whose hops each outlast the window, so a tab can never
 * bounce back and forth without end. A person who comes back to this host
 * after the short window is moved again.
 */

import { isCliAuthFlowLocation } from "@/lib/cli-auth-flow";

// Moves are stored as { [moveKey]: epoch ms[] }. The key differs from the old
// permanent list ("organizationHostMoves") so tabs holding it are not stuck.
const HOST_MOVES_KEY = "organizationHostMoveTimes";

/**
 * How long a recorded move blocks another automatic move of the same
 * organization to the same host. A redirect loop usually repeats within
 * seconds.
 */
const HOST_MOVE_WINDOW_MS = 15_000;

/**
 * At most HOST_MOVE_LIMIT automatic moves of the same organization to the
 * same host per HOST_MOVE_LIMIT_WINDOW_MS. This bounds a loop whose hops are
 * each slower than HOST_MOVE_WINDOW_MS.
 */
const HOST_MOVE_LIMIT = 3;
const HOST_MOVE_LIMIT_WINDOW_MS = 10 * 60_000;

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
 * on the current path and query there, or undefined when the dashboard should
 * stay where it is. The hash is dropped: the transfer URL is server-visible and
 * logged, and a fragment can carry secrets that never left the browser before.
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
    redirect: current.pathname + current.search,
  });
  return `${target.origin}/rpc/auth.transferIn?${params.toString()}`;
}

function moves(): Record<string, number[]> {
  try {
    const parsed = JSON.parse(
      sessionStorage.getItem(HOST_MOVES_KEY) ?? "{}",
    ) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }
    const result: Record<string, number[]> = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (!Array.isArray(value)) continue;
      const times = value.filter(
        (t): t is number => typeof t === "number" && Number.isFinite(t),
      );
      if (times.length > 0) result[key] = times;
    }
    return result;
  } catch {
    return {};
  }
}

/** Whether a move recorded at movedAt falls within windowMs before now. */
function within(movedAt: number, now: number, windowMs: number): boolean {
  // A clock that went backwards counts as recent, which keeps the guard on.
  return now - movedAt < windowMs;
}

/**
 * Identifies moving organizationId to target's host. Moves are recorded per
 * organization, so switching back to another organization that lives on a
 * host this tab already visited still moves the tab.
 */
export function moveKey(organizationId: string, target: string): string {
  return `${organizationId} ${new URL(target).host}`;
}

/**
 * Whether this tab's guard blocks the move from this host: one was made in the
 * short window, or the limit was reached in the longer one.
 */
export function alreadyMoved(key: string, now = Date.now()): boolean {
  const times = moves()[key] ?? [];
  return (
    times.some((t) => within(t, now, HOST_MOVE_WINDOW_MS)) ||
    times.filter((t) => within(t, now, HOST_MOVE_LIMIT_WINDOW_MS)).length >=
      HOST_MOVE_LIMIT
  );
}

/**
 * Records the move in this host's tab storage before leaving, so a tab that
 * comes straight back here with the same organization is not sent off again.
 * Moves older than the longer window are dropped. Returns false when storage
 * is unavailable: without the guard the move is not safe to make.
 */
export function recordMove(key: string, now = Date.now()): boolean {
  try {
    const kept: Record<string, number[]> = {};
    for (const [k, times] of Object.entries(moves())) {
      const recent = times.filter((t) =>
        within(t, now, HOST_MOVE_LIMIT_WINDOW_MS),
      );
      if (recent.length > 0) kept[k] = recent;
    }
    kept[key] = [...(kept[key] ?? []), now];
    sessionStorage.setItem(HOST_MOVES_KEY, JSON.stringify(kept));
    return true;
  } catch {
    return false;
  }
}
