import { connectionState } from "@/lib/connection-state";
import { subjectLabel } from "@/lib/user-session-status";

import type { UserSession } from "@gram/client/models/components/usersession.js";

/**
 * What a row is filed under. "Person" is the default because the question an
 * admin arrives with is almost always about someone — who has access, and what
 * can they reach — rather than about a credential.
 */
export type ConnectionGrouping = "subject" | "provider" | "client";

export const CONNECTION_GROUPING_LABELS: Record<ConnectionGrouping, string> = {
  subject: "Person",
  provider: "Provider",
  client: "Client",
};

export type ConnectionGroup = {
  key: string;
  label: string;
  sessions: UserSession[];
  liveCount: number;
  attentionCount: number;
  /** Active session ids, the only ones a revoke action can act on. */
  revocableIds: string[];
};

/**
 * A session can belong to several provider groups at once — one Gram server can
 * front several upstreams — so grouping by provider intentionally repeats a
 * session under each provider it reaches. Grouping by person or client always
 * files a session exactly once.
 */
function groupKeysFor(
  session: UserSession,
  grouping: ConnectionGrouping,
): { key: string; label: string }[] {
  switch (grouping) {
    case "subject":
      return [{ key: session.subjectUrn, label: subjectLabel(session) }];
    case "client": {
      const label = session.clientName ?? "Unknown client";
      return [{ key: session.userSessionClientId ?? label, label }];
    }
    case "provider": {
      const upstreams = session.upstreams ?? [];
      if (upstreams.length === 0) {
        return [{ key: "__none__", label: "No upstream provider" }];
      }
      return upstreams.map((upstream) => ({
        key: upstream.remoteSessionIssuerId,
        label: upstream.issuerSlug,
      }));
    }
  }
}

/**
 * Groups sessions for display, ordering groups by the ones most likely to need
 * attention: anything broken first, then the busiest, then alphabetically so
 * the order is stable between refreshes.
 */
export function groupConnections(
  sessions: UserSession[],
  grouping: ConnectionGrouping,
  now: number = Date.now(),
): ConnectionGroup[] {
  const groups = new Map<string, ConnectionGroup>();

  for (const session of sessions) {
    for (const { key, label } of groupKeysFor(session, grouping)) {
      let group = groups.get(key);
      if (!group) {
        group = {
          key,
          label,
          sessions: [],
          liveCount: 0,
          attentionCount: 0,
          revocableIds: [],
        };
        groups.set(key, group);
      }

      group.sessions.push(session);

      const state = connectionState(session, now);
      if (state === "live") group.liveCount += 1;
      if (state === "expiring" || state === "needs_reauth") {
        group.attentionCount += 1;
      }
      if (state !== "revoked" && state !== "needs_reauth") {
        group.revocableIds.push(session.id);
      }
    }
  }

  return [...groups.values()].sort((a, b) => {
    if (a.attentionCount !== b.attentionCount) {
      return b.attentionCount - a.attentionCount;
    }
    if (a.sessions.length !== b.sessions.length) {
      return b.sessions.length - a.sessions.length;
    }
    return a.label.localeCompare(b.label);
  });
}

/**
 * Group subtitle: the counts worth acting on. Silent when there is nothing to
 * say, so a healthy list stays quiet rather than repeating "0 needs attention"
 * on every row.
 */
export function connectionGroupSummary(group: ConnectionGroup): string {
  const parts: string[] = [];
  if (group.liveCount > 0) parts.push(`${group.liveCount} live`);
  if (group.attentionCount > 0) {
    parts.push(`${group.attentionCount} needs attention`);
  }
  if (parts.length === 0) {
    return `${group.sessions.length} connection${group.sessions.length === 1 ? "" : "s"}`;
  }
  return parts.join(" · ");
}
