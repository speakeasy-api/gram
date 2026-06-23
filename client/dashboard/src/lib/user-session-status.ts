import { format, formatDistanceToNow } from "date-fns";

import type { UserSession } from "@gram/client/models/components";

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
  if (new Date(session.expiresAt).getTime() <= Date.now()) return "expired";
  return "active";
}

export const STATUS_PRESENTATION: Record<
  SessionStatus,
  { label: string; badgeVariant: BadgeVariant; dotClass: string }
> = {
  active: {
    label: "Active",
    badgeVariant: "success",
    dotClass: "bg-emerald-500",
  },
  expired: {
    label: "Expired",
    badgeVariant: "neutral",
    dotClass: "bg-muted-foreground",
  },
  revoked: {
    label: "Revoked",
    badgeVariant: "destructive",
    dotClass: "bg-destructive",
  },
};

// Human-readable timing label that matches the session's status, so an expired
// session never reads "expires ... ago". Shared by the list and table rows.
export function sessionTimeLabel(session: UserSession): string {
  const status = sessionStatus(session);
  if (status === "revoked" && session.revokedAt) {
    return `revoked ${format(new Date(session.revokedAt), "PP")}`;
  }
  const relative = formatDistanceToNow(new Date(session.expiresAt), {
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
