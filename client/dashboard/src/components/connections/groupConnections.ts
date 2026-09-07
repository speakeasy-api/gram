import {
  connectionIsInactive,
  connectionLastUsedAt,
  connectionState,
  type ConnectionState,
} from "@/lib/connection-state";
import { providerLabel } from "@/lib/provider-label";
import type { CredentialKind } from "@/lib/user-session-client-credential";
import { subjectLabel } from "@/lib/user-session-status";

import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import type { UserSessionUpstream } from "@gram/client/models/components/usersessionupstream.js";

/**
 * What a row is filed under. "Person" is the default because the question an
 * admin arrives with is almost always about someone — who has access, and what
 * can they reach — rather than about a credential.
 */
export type ConnectionGrouping = "subject" | "issuer" | "provider" | "client";

// "Agent" rather than "client": the OAuth client on the other side of a
// connection is an agent, and that is what it is called everywhere else in the
// product. `client` stays as the key, which names the protocol record.
export const CONNECTION_GROUPING_LABELS: Record<ConnectionGrouping, string> = {
  subject: "Person",
  // The Gram MCP server the session was issued through, which is what the rest
  // of the product means by "MCP server" — distinct from "Provider", the
  // upstream the server holds tokens for.
  issuer: "MCP server",
  provider: "Provider",
  client: "Agent",
};

export type ConnectionGroup = {
  key: string;
  label: string;
  sessions: UserSession[];
  liveCount: number;
  attentionCount: number;
  /** Active session ids, the only ones a revoke action can act on. */
  revocableIds: string[];
  /**
   * The most recent traffic across the group's connections, or null when none of
   * them has a recorded use. Ordering keys off this, so a group is only as stale
   * as its liveliest connection.
   */
  lastUsedAt: number | null;
  /**
   * True when every connection in the group is dormant or unusable — including
   * the degenerate case of a registration holding no connections at all, which
   * is as inactive as a group gets.
   */
  inactive: boolean;
  /**
   * Set when the group heading names a person, so the header can show their
   * face. Absent for provider and client groups, which are not identities and
   * would read oddly with an initials badge.
   */
  // `urn` is the subject URN the sessions were filed under, which is also the
  // identity URN the person's page resolves from.
  identity?: { photoUrl?: string; urn?: string };
  /**
   * The registration this group stands for, under client grouping. Carrying the
   * whole record (rather than an id) lets the header offer "revoke
   * registration" — cutting off *future* connections, which revoking live
   * sessions does not do.
   */
  client?: UserSessionClient;

  /**
   * Id of the registration this group stands for, under client grouping. Set
   * from either source: the caller may not have passed `clients` (the
   * organization and employee pages do not), in which case the group is derived
   * from sessions alone and this is the only handle on the registration it has.
   */
  clientId?: string;

  /**
   * What the registration must present to authenticate, under client grouping.
   * Every session in the group was issued through the same registration, so any
   * of them reports the same kind; a group keyed on a client name because its
   * sessions carry no client id has none.
   */
  credentialKind?: CredentialKind;

  /**
   * The raw token_endpoint_auth_method the registration declared, under client
   * grouping. Absent for a registration that predates the recorded method.
   */
  declaredAuthMethod?: string;
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
    case "issuer":
      return [
        {
          key: session.userSessionIssuerId,
          // The slug is the server's own name; unlike an upstream's it needs
          // no provider lookup to read.
          label: session.issuerSlug,
        },
      ];
    case "client": {
      const label = session.clientName ?? "Unknown client";
      return [{ key: session.userSessionClientId ?? label, label }];
    }
    case "provider": {
      const upstreams = session.upstreams ?? [];
      if (upstreams.length === 0) {
        return [{ key: "__none__", label: "No upstream provider" }];
      }
      // Deduplicated on issuer: one issuer can have several
      // remote_session_clients attached, so a subject may hold two upstreams
      // that resolve to the same provider. Ungrouped, that filed the session
      // twice under one heading and double-counted its connections.
      const byIssuer = new Map<string, string>();
      for (const upstream of upstreams) {
        byIssuer.set(
          upstream.remoteSessionIssuerId,
          providerLabel(upstream.issuerSlug),
        );
      }
      return [...byIssuer].map(([key, label]) => ({ key, label }));
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
  options: { clients?: UserSessionClient[]; now?: number } = {},
): ConnectionGroup[] {
  const now = options.now ?? Date.now();
  const groups = new Map<string, ConnectionGroup>();

  // Seed a group per registration so a client that has never been used still
  // appears — and stays revocable. Derived from sessions alone it would be
  // invisible, since there is no connection to group under it.
  if (grouping === "client") {
    for (const client of options.clients ?? []) {
      groups.set(client.id, {
        key: client.id,
        label: client.clientName,
        sessions: [],
        liveCount: 0,
        attentionCount: 0,
        revocableIds: [],
        lastUsedAt: null,
        inactive: true,
        identity: undefined,
        client,
        clientId: client.id,
        credentialKind: client.credentialKind,
        declaredAuthMethod: client.tokenEndpointAuthMethod,
      });
    }
  }

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
          lastUsedAt: null,
          // Narrowed to false by the first active session filed under it.
          inactive: true,
          // Only the person grouping names an identity. A user subject may
          // still have no photo, in which case the header falls back to
          // initials rather than omitting the avatar.
          identity:
            grouping === "subject" && session.subjectType === "user"
              ? {
                  photoUrl: session.subjectPhotoUrl ?? undefined,
                  urn: session.subjectUrn,
                }
              : undefined,
          client: undefined,
          // Both read off the session rather than a registration record, which
          // only the MCP server tab supplies. Every session filed under one
          // agent group was issued through the same registration, so the first
          // one to create the group speaks for all of them.
          clientId:
            grouping === "client"
              ? (session.userSessionClientId ?? undefined)
              : undefined,
          credentialKind:
            grouping === "client" ? session.clientCredentialKind : undefined,
          declaredAuthMethod:
            grouping === "client"
              ? session.clientTokenEndpointAuthMethod
              : undefined,
        };
        groups.set(key, group);
      }

      group.sessions.push(session);

      const state = connectionState(session, now);
      if (state === "live") group.liveCount += 1;
      if (state === "expiring" || state === "needs_reauth") {
        group.attentionCount += 1;
      }
      // Anything not already revoked can be revoked, needs_reauth included:
      // a connection whose upstream grant died is exactly the one an admin
      // most wants to cut off, and excluding it left those rows with no
      // action at all.
      if (state !== "revoked") {
        group.revocableIds.push(session.id);
      }

      const lastUsedAt = connectionLastUsedAt(session);
      if (lastUsedAt !== null) {
        group.lastUsedAt = Math.max(group.lastUsedAt ?? lastUsedAt, lastUsedAt);
      }
      if (!connectionIsInactive(session, now)) group.inactive = false;
    }
  }

  // Sub-rows read in the same order as the rows they hang off.
  for (const group of groups.values()) {
    group.sessions.sort((a, b) =>
      byRecencyDescending(
        { lastUsedAt: connectionLastUsedAt(a), label: subjectLabel(a) },
        { lastUsedAt: connectionLastUsedAt(b), label: subjectLabel(b) },
      ),
    );
  }

  return [...groups.values()].sort(byRecencyDescending);
}

/**
 * Most recently used first, with anything unused trailing alphabetically.
 *
 * Recency is the ordering everywhere now that connections record it: it answers
 * "what is actually in use here" without the reader decoding a status column,
 * and it degrades gracefully — the never-used sink to the bottom instead of
 * claiming the epoch. Anything needing attention is separated out by the
 * active/inactive split rather than by pushing it up the list.
 */
function byRecencyDescending(
  a: { lastUsedAt: number | null; label?: string },
  b: { lastUsedAt: number | null; label?: string },
): number {
  if (a.lastUsedAt !== b.lastUsedAt) {
    if (a.lastUsedAt === null) return 1;
    if (b.lastUsedAt === null) return -1;
    return b.lastUsedAt - a.lastUsedAt;
  }
  return (a.label ?? "").localeCompare(b.label ?? "");
}

/**
 * Splits a grouped list into the connections doing work and the ones that are
 * not, preserving the recency order within each.
 */
export function splitByActivity(groups: ConnectionGroup[]): {
  active: ConnectionGroup[];
  inactive: ConnectionGroup[];
} {
  return {
    active: groups.filter((group) => !group.inactive),
    inactive: groups.filter((group) => group.inactive),
  };
}

/**
 * Group subtitle: the counts worth acting on. Silent when there is nothing to
 * say, so a healthy list stays quiet rather than repeating "0 needs attention"
 * on every row.
 */
/**
 * The single state a group's status column reports — the worst one present.
 * Returns null when nothing needs attention, which is what keeps a healthy row
 * free of tone entirely.
 */
export function groupAttentionState(
  group: ConnectionGroup,
  now: number = Date.now(),
): ConnectionState | null {
  let worst: ConnectionState | null = null;
  for (const session of group.sessions) {
    const state = connectionState(session, now);
    if (state === "needs_reauth") return "needs_reauth";
    if (state === "expiring") worst = "expiring";
  }
  return worst;
}

export function connectionGroupSummary(group: ConnectionGroup): string {
  if (group.sessions.length === 0) return "No connections";

  // Just the count. What is wrong, if anything, is reported in its own column
  // so a healthy row carries no problem-shaped text at all.
  return `${group.sessions.length} connection${group.sessions.length === 1 ? "" : "s"}`;
}

/**
 * The upstream providers this group's subject holds tokens for.
 *
 * Deduplicated by remote session, because remote_sessions are keyed on
 * (subject_urn, user_session_issuer_id) rather than on an individual
 * user_session: every client a person connects through reports the same
 * upstreams. Listing them per connection would claim each client routes to
 * every provider, which is not what the join says.
 */
export function groupUpstreams(group: ConnectionGroup): UserSessionUpstream[] {
  const byId = new Map<string, UserSessionUpstream>();
  for (const session of group.sessions) {
    for (const upstream of session.upstreams ?? []) {
      byId.set(upstream.remoteSessionId, upstream);
    }
  }
  return [...byId.values()];
}
