import { formatDistanceToNow } from "date-fns";

import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionUpstream } from "@gram/client/models/components/usersessionupstream.js";

/**
 * A brokered connection is two legs — an agent's session against a Gram MCP
 * server, and the upstream tokens Gram holds on that subject's behalf — and its
 * health is the worse of the two. A session whose own refresh token is healthy
 * still can't do anything useful if the upstream it fronts has lost its grant.
 *
 * Ordered worst-first: the row shows one state, so the states are ranked and
 * the most severe applicable one wins.
 */
export type ConnectionState =
  | "revoked"
  | "needs_reauth"
  | "expiring"
  | "idle"
  | "live";

export type ConnectionTone =
  | "neutral"
  | "success"
  | "information"
  | "warning"
  | "destructive";

/**
 * How recently a connection must have carried traffic to read as live. The
 * backend coalesces last_used_at writes to a five-minute window, so anything
 * below that would report staleness that is really just write granularity.
 */
const LIVE_WINDOW_MS = 15 * 60 * 1000;

/**
 * How close a hard deadline must be to read as expiring. A day is enough
 * warning for an admin to act before an agent starts failing, and short enough
 * that the state stays rare rather than becoming the default.
 */
const EXPIRING_WINDOW_MS = 24 * 60 * 60 * 1000;

export const CONNECTION_STATE_PRESENTATION: Record<
  ConnectionState,
  { label: string; tone: ConnectionTone; toneClass: string }
> = {
  live: {
    label: "Live",
    tone: "success",
    toneClass: "text-default-success",
  },
  idle: {
    label: "Idle",
    tone: "neutral",
    toneClass: "text-muted-foreground",
  },
  expiring: {
    label: "Expiring",
    tone: "warning",
    toneClass: "text-default-warning",
  },
  needs_reauth: {
    label: "Needs re-auth",
    tone: "destructive",
    toneClass: "text-default-destructive",
  },
  revoked: {
    label: "Revoked",
    tone: "neutral",
    toneClass: "text-muted-foreground",
  },
};

/**
 * The generated SDK deserializes `date-time` fields to `Date`, but callers also
 * hold raw ISO strings (route params, test fixtures), so both are accepted.
 * Returns null for an absent or unparseable value — absent means "no deadline",
 * which must never collapse to a deadline at the epoch.
 */
type Instant = Date | string | null | undefined;

function msUntil(value: Instant, now: number): number | null {
  if (!value) return null;
  const at =
    value instanceof Date ? value.getTime() : new Date(value).getTime();
  return Number.isNaN(at) ? null : at - now;
}

/**
 * An upstream is unusable once its access token has expired and it holds no
 * refresh grant to mint another. `hasRefreshToken` rather than
 * `refreshExpiresAt`, because an upstream may issue a refresh token with no
 * expiry at all — a null expiry there means "never expires", not "absent".
 */
function upstreamNeedsReauth(
  upstream: UserSessionUpstream,
  now: number,
): boolean {
  if (upstream.hasRefreshToken) {
    const refreshRemaining = msUntil(upstream.refreshExpiresAt, now);
    if (refreshRemaining !== null && refreshRemaining <= 0) return true;
    const authRemaining = msUntil(upstream.authorizationExpiresAt, now);
    return authRemaining !== null && authRemaining <= 0;
  }

  const accessRemaining = msUntil(upstream.accessExpiresAt, now);
  return accessRemaining !== null && accessRemaining <= 0;
}

/**
 * The soonest hard deadline on an upstream. `authorizationExpiresAt` counts
 * even when a refresh grant is held: exchanging a token slides the refresh
 * deadline but never the absolute authorization one, so an auto-refreshing
 * connection can still be days from forcing the user back through consent.
 */
function upstreamDeadline(
  upstream: UserSessionUpstream,
  now: number,
): number | null {
  const candidates = [
    msUntil(upstream.authorizationExpiresAt, now),
    upstream.hasRefreshToken
      ? msUntil(upstream.refreshExpiresAt, now)
      : msUntil(upstream.accessExpiresAt, now),
  ].filter((value): value is number => value !== null);

  if (candidates.length === 0) return null;
  return Math.min(...candidates);
}

/**
 * Resolves the single state a connection row reports.
 *
 * `now` is injected rather than read from the clock so the mapping stays pure
 * and testable.
 */
export function connectionState(
  session: UserSession,
  now: number = Date.now(),
): ConnectionState {
  if (session.revokedAt) return "revoked";

  const sessionRemaining = msUntil(session.refreshExpiresAt, now);
  if (sessionRemaining !== null && sessionRemaining <= 0) return "needs_reauth";

  const upstreams = session.upstreams ?? [];
  if (upstreams.some((upstream) => upstreamNeedsReauth(upstream, now))) {
    return "needs_reauth";
  }

  const deadlines = [
    sessionRemaining,
    ...upstreams.map((upstream) => upstreamDeadline(upstream, now)),
  ].filter((value): value is number => value !== null);

  if (deadlines.some((remaining) => remaining <= EXPIRING_WINDOW_MS)) {
    return "expiring";
  }

  const usedAgo = msUntil(session.lastUsedAt, now);
  // Negative because last_used_at is in the past; null means the session
  // predates the column, which is unknown rather than unused.
  if (usedAgo !== null && -usedAgo <= LIVE_WINDOW_MS) return "live";

  return "idle";
}

/**
 * Activity phrasing for a connection. Distinguishes "we know it has not been
 * used" from "we have no record", because a session that predates last_used_at
 * would otherwise read as permanently dormant.
 */
export function connectionActivityLabel(lastUsedAt: Instant): string {
  if (!lastUsedAt) return "no recorded use";
  return `used ${formatDistanceToNow(new Date(lastUsedAt), { addSuffix: true })}`;
}

/**
 * Deadline phrasing for the whole connection: the soonest thing that will
 * interrupt it, named so an admin knows which clock is running out.
 */
export function connectionDeadlineLabel(
  session: UserSession,
  now: number = Date.now(),
): string {
  if (session.revokedAt) return "revoked";

  const sessionRemaining = msUntil(session.refreshExpiresAt, now);
  const upstreams = session.upstreams ?? [];

  const withoutRefresh = upstreams.find(
    (upstream) => !upstream.hasRefreshToken && upstream.accessExpiresAt,
  );
  if (withoutRefresh) {
    const remaining = msUntil(withoutRefresh.accessExpiresAt, now);
    if (remaining !== null && remaining <= 0) return "no refresh grant";
  }

  if (sessionRemaining === null) return "no expiry";
  const relative = formatDistanceToNow(new Date(session.refreshExpiresAt), {
    addSuffix: true,
  });
  return sessionRemaining <= 0 ? `expired ${relative}` : `expires ${relative}`;
}
