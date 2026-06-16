import type { UserSession } from "@gram/client/models/components";

export type SessionStatus = "active" | "expired" | "revoked";

export function sessionStatus(session: UserSession): SessionStatus {
  if (session.revokedAt) return "revoked";
  if (new Date(session.expiresAt).getTime() <= Date.now()) return "expired";
  return "active";
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
