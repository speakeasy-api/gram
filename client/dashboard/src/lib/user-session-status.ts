import type { ComponentProps } from "react";

import type { Badge } from "@/components/ui/badge";
import type { UserSession } from "@gram/client/models/components";

export type SessionStatus = "active" | "expired" | "revoked";

type BadgeVariant = ComponentProps<typeof Badge>["variant"];

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
    badgeVariant: "default",
    dotClass: "bg-emerald-500",
  },
  expired: {
    label: "Expired",
    badgeVariant: "secondary",
    dotClass: "bg-muted-foreground",
  },
  revoked: {
    label: "Revoked",
    badgeVariant: "destructive",
    dotClass: "bg-destructive",
  },
};

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
