import type { UserSession } from "@gram/client/models/components/usersession.js";

/**
 * How a session's subject reads on screen.
 *
 * A subject URN is the fallback rather than the answer: it is exact but
 * unreadable, so it only appears when the directory gave us no display name and
 * the subject type has no better word of its own.
 */
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
