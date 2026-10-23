import { formatDistance } from "date-fns";

import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import { dateTimeFormatters } from "@/lib/dates";

/**
 * How long a person's upstream connection keeps working without them, read
 * from the expiry fields a remote session carries.
 *
 * A connection with a refresh token renews its access on use, so the deadline
 * that matters is the refresh token's, and an upstream that issues one with no
 * expiry has no deadline at all. Without a refresh token the access token's
 * expiry is the end, and nothing renews it.
 */
export type ConnectionLifetime =
  /** No readable expiry was recorded, so nothing can be said about its end. */
  | { kind: "unknown" }
  | { kind: "refreshes" }
  | { kind: "refreshes-until"; at: Date; soon: boolean }
  | { kind: "expires"; at: Date; soon: boolean }
  | { kind: "ended"; at: Date };

/**
 * How close a deadline must be to read as ending soon. A day is enough warning
 * to sign in again before an agent's calls start failing, and the same window
 * the connection health labels use.
 */
const ENDING_SOON_MS = 24 * 60 * 60 * 1000;

type SessionExpiry = Pick<
  RemoteSession,
  "accessExpiresAt" | "refreshExpiresAt" | "hasRefreshToken"
>;

/** A recorded instant, or undefined when it is absent or unreadable. */
function instant(value: Date | string | undefined): Date | undefined {
  if (value === undefined) return undefined;
  const at = value instanceof Date ? value : new Date(value);
  return Number.isNaN(at.getTime()) ? undefined : at;
}

export function connectionLifetime(
  session: SessionExpiry,
  now: number = Date.now(),
): ConnectionLifetime {
  const access = instant(session.accessExpiresAt);
  if (session.hasRefreshToken) {
    const refresh = instant(session.refreshExpiresAt);
    if (!refresh) return { kind: "refreshes" };
    if (refresh.getTime() > now)
      return {
        kind: "refreshes-until",
        at: refresh,
        soon: refresh.getTime() - now <= ENDING_SOON_MS,
      };
    // A lapsed refresh token still leaves the access token it last minted,
    // which works until its own expiry and is then the end.
  }
  if (!access) return { kind: "unknown" };
  if (access.getTime() > now)
    return {
      kind: "expires",
      at: access,
      soon: access.getTime() - now <= ENDING_SOON_MS,
    };
  return { kind: "ended", at: access };
}

/** Whether a connection stops working within the day, so it reads as a warning. */
export function lifetimeEndingSoon(
  lifetime: ConnectionLifetime | undefined,
): boolean {
  if (lifetime === undefined) return false;
  if (lifetime.kind === "refreshes-until" || lifetime.kind === "expires")
    return lifetime.soon;
  return false;
}

/** The connection whose deadline comes first, which bounds the whole set. */
export function soonestLifetime(
  lifetimes: ConnectionLifetime[],
): ConnectionLifetime | undefined {
  const rank = (lifetime: ConnectionLifetime) =>
    "at" in lifetime ? lifetime.at.getTime() : Number.POSITIVE_INFINITY;
  return [...lifetimes].sort((a, b) => rank(a) - rank(b))[0];
}

/**
 * One line that says when a connection stops working. Said in the terms a
 * reader acts on: whether it renews itself, and the day it no longer does.
 */
export function connectionLifetimeLabel(
  lifetime: ConnectionLifetime,
  now: number = Date.now(),
): string {
  switch (lifetime.kind) {
    case "unknown":
      return "Expiry not recorded";
    case "refreshes":
      return "Refreshes itself, no end date";
    case "refreshes-until":
      return `Refreshes itself until ${dateTimeFormatters.day.format(lifetime.at)}`;
    case "expires":
      return `Expires ${dateTimeFormatters.day.format(lifetime.at)} (${formatDistance(lifetime.at, now, { addSuffix: true })}) and does not refresh`;
    case "ended":
      return `Ended ${formatDistance(lifetime.at, now, { addSuffix: true })}`;
  }
}
