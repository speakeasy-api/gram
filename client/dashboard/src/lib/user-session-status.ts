import { format, formatDistanceToNow } from "date-fns";

import type { UserSession } from "@gram/client/models/components/usersession.js";

export type SessionStatus = "active" | "expired" | "revoked";

// Moonshine Badge semantic variants ("our brand badges"). The names are hooks
// onto the brand palette, not literal alert semantics.
type BadgeVariant =
  | "neutral"
  | "success"
  | "information"
  | "warning"
  | "destructive";

export function sessionStatus(session: UserSession): SessionStatus {
  if (session.revokedAt) return "revoked";
  // Active/expired is keyed off refreshExpiresAt (the session/refresh
  // lifetime), NOT expiresAt (the ~1h access-token lifetime), matching the
  // backend's ListUserSessionsByProjectID status filter. An active MCP
  // connection only refreshes its access token on demand, so a live session
  // routinely has a past expiresAt while its refresh token is still valid;
  // keying "active" off expiresAt makes those connections wrongly read as
  // expired until the client next refreshes.
  if (new Date(session.refreshExpiresAt).getTime() <= Date.now())
    return "expired";
  return "active";
}

export const STATUS_PRESENTATION: Record<
  SessionStatus,
  { label: string; badgeVariant: BadgeVariant }
> = {
  active: {
    label: "Active",
    badgeVariant: "success",
  },
  expired: {
    label: "Expired",
    badgeVariant: "neutral",
  },
  revoked: {
    label: "Revoked",
    badgeVariant: "destructive",
  },
};

// Human-readable timing label that matches the session's status, so an expired
// session never reads "expires ... ago". Shared by the list and table rows.
export function sessionTimeLabel(session: UserSession): string {
  const status = sessionStatus(session);
  if (status === "revoked" && session.revokedAt) {
    return `revoked ${format(new Date(session.revokedAt), "PP")}`;
  }
  const relative = formatDistanceToNow(new Date(session.refreshExpiresAt), {
    addSuffix: true,
  });
  return status === "expired" ? `expired ${relative}` : `expires ${relative}`;
}

export function subjectLabel(session: UserSession): string {
  if (session.subjectDisplayName) return session.subjectDisplayName;
  switch (session.subjectType) {
    case "apikey":
      return "API key";
    case "anonymous":
      return "Anonymous client";
    default:
      return session.subjectUrn;
  }
}
