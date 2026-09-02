import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { fetchIdentityPeers, identityPeersQueryKey } from "./identityPeers";

import { fetchIdentityRoster, identityRosterQueryKey } from "./identityRoster";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import { useAuditLogs } from "@gram/client/react-query/auditLogs.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useManagedDevices } from "@gram/client/react-query/managedDevices.js";
import {
  buildGetUserMetricsSummaryQuery,
  useGetUserMetricsSummary,
} from "@gram/client/react-query/getUserMetricsSummary.js";
import { useRiskUserBreakdown } from "@gram/client/react-query/riskUserBreakdown.js";
import { useShadowMCPInventoryServersForUser } from "@gram/client/react-query/shadowMCPInventoryServersForUser.js";

const OFF = { throwOnError: false } as const;

/** What access.listShadowMCPInventoryServersForUser accepts in one request. */
const MAX_USER_KEYS = 200;

/** The part of a query a retry button needs. */
type RetryableQuery = { isError: boolean; refetch: () => unknown };

/**
 * A retry that re-runs only the reads which actually failed.
 *
 * Several queries on these pages are held behind `enabled` — no Gram user id,
 * no chat:read, no org:admin — and `refetch()` ignores `enabled` and fires
 * anyway. A retry button that called it blindly would ask for audit logs with
 * no actor filter, or for chats the caller may not filter by user, and render
 * either under this identity's name. A query that is disabled never reaches an
 * error, so having errored is itself proof it was allowed to run.
 */
export function retryFailed(...queries: RetryableQuery[]): () => void {
  return () => {
    for (const query of queries) {
      if (query.isError) void query.refetch();
    }
  };
}

/**
 * The window every identity panel reads, kept in the URL so a link to a
 * sub-page carries the range the sender was looking at.
 */
export function useIdentityWindow(): { from: Date; to: Date } {
  const { from, to } = useDateRangeFilter();
  return { from, to };
}

/**
 * The project the telemetry panels read.
 *
 * The page lives under a project, so this is the route's project rather than a
 * filter of its own. Usage, chats, cost and risk are all recorded per project,
 * and the segment in the address is what says which slice is on screen.
 */
export function useIdentityProject(): { slug: string; id: string } {
  const project = useProject();
  return { slug: project.slug, id: project.id };
}

/**
 * Whether telemetry can be asked about this identity at all.
 *
 * The summary endpoint keys on a Gram user id or an agent-reported id, and an
 * identity carrying neither — an api-key subject, say — is not a subject it
 * can answer for. No request is made, so the tiles have nothing to show and
 * must say that rather than stand at zero.
 */
export function hasMetricsSubject(identity: IdentityModel): boolean {
  return !!(identity.userIds[0] || identity.externalUserIds[0]);
}

/**
 * Telemetry keys usage on either the Gram user id or the id an agent reported,
 * and the endpoint takes exactly one of them, so prefer the directory user and
 * fall back to the agent identifier for subjects with no directory row.
 */
export function useIdentityMetrics(
  identity: IdentityModel,
  from: Date,
  to: Date,
): ReturnType<typeof useGetUserMetricsSummary> {
  const client = useGramContext();
  const { slug: gramProject } = useIdentityProject();
  const userId = identity.userIds[0];
  const externalUserId = identity.externalUserIds[0];
  const request = {
    gramProject,
    getUserMetricsSummaryPayload: {
      from,
      to,
      ...(userId ? { userId } : externalUserId ? { externalUserId } : {}),
    },
  };

  // Built rather than called through useGetUserMetricsSummary, purely to widen
  // the cache key. This endpoint takes its subject and window in the request
  // body, and the generated key covers only the project and auth headers, so
  // every identity and every range in one project share a single cache entry:
  // the tiles would keep the first answer they got and the range picker would
  // look inert on exactly the figures this page leads with. The generated
  // queryFn still does the fetching; only the key is ours.
  const built = buildGetUserMetricsSummaryQuery(client, request);
  return useQuery({
    ...built,
    queryKey: [
      ...built.queryKey,
      {
        userId: userId ?? null,
        externalUserId: externalUserId ?? null,
        from: from.toISOString(),
        to: to.toISOString(),
      },
    ],
    throwOnError: false,
    enabled: !!userId || !!externalUserId,
  });
}

/**
 * Whether this viewer can be shown someone else's sessions at all.
 *
 * chat.list honours an explicit user filter only for a caller holding an
 * unrestricted chat:read grant;;without it the filter is discarded and the
 * caller's OWN sessions come back instead. Rendering those under the subject's
 * name would be a silent misattribution, so the panel asks for nothing rather
 * than asking for something it cannot trust.
 */
/**
 * Whether anything in the org has actually been seen under this identity.
 *
 * The resolver answers structurally: hand it any well-formed URN and it returns
 * a subject, so a typo or a stale link renders a complete page for a person who
 * does not exist. A directory row settles it; for everyone else the question is
 * whether telemetry has ever recorded the identifier, which is a targeted
 * lookup rather than the roster crawl the index does.
 */
export function useIdentityIsKnown(identity: IdentityModel | undefined): {
  known: boolean;
  isPending: boolean;
  /**
   * Whether the roster read failed. `known` is false either way, and callers
   * that word that as a finding — "no activity recorded", "not enrolled" —
   * need to tell the two apart.
   */
  isError: boolean;
  refetch: () => void;
} {
  const client = useGramContext();
  const organization = useOrganization();
  const { slug: projectSlug } = useIdentityProject();
  const hasDirectoryRow = (identity?.userIds.length ?? 0) > 0;
  const identifiers = new Set(
    [...(identity?.emails ?? []), ...(identity?.externalUserIds ?? [])].map(
      (value) => value.toLowerCase(),
    ),
  );
  // The same roster the index lists, under the same query key, so this is a
  // cache hit whenever the reader arrived from there and one fetch otherwise.
  // Asking searchUsers for the identifier directly does not work: the roster
  // surfaces email-keyed identities through the agent-metrics view and
  // id-keyed ones from raw logs, and the id filter matches neither.
  const query = useQuery({
    queryKey: identityRosterQueryKey(organization.id, projectSlug),
    queryFn: () => fetchIdentityRoster(client, projectSlug),
    throwOnError: false,
    enabled: !!identity && !hasDirectoryRow && identifiers.size > 0,
  });

  const outcome = {
    isError: query.isError,
    refetch: () => void query.refetch(),
  };

  if (!identity) return { known: true, isPending: true, ...outcome };
  if (hasDirectoryRow) return { known: true, isPending: false, ...outcome };
  // Only an unattributed subject is the "identifier nothing was recorded
  // under" case: an api-key or agent identity legitimately carries neither an
  // address nor an agent id, and is still a real subject.
  if (identity.kind !== "unattributed")
    return { known: true, isPending: false, ...outcome };
  if (identifiers.size === 0)
    return { known: false, isPending: false, ...outcome };
  if (query.isPending) return { known: false, isPending: true, ...outcome };
  return {
    known: (query.data ?? []).some(
      (summary) =>
        // Independently, not `userEmail || userId`: a row can carry both, and
        // preferring the address there would miss an identity we only know by
        // the id an agent reported — which renders as "never seen here".
        (!!summary.userEmail &&
          identifiers.has(summary.userEmail.toLowerCase())) ||
        (!!summary.userId && identifiers.has(summary.userId.toLowerCase())),
    ),
    isPending: false,
    ...outcome,
  };
}

/**
 * Risk findings and the shadow-MCP inventory are org:admin surfaces on their
 * own pages, and their endpoints enforce that. A reader without it is shown
 * nothing rather than an empty panel that reads as "no findings".
 */
export function useCanReadRisk(): boolean {
  const { hasScope } = useRBAC();
  return hasScope("org:admin");
}

/**
 * Whether the identity on screen is the person reading the page.
 *
 * The directory id settles it. The address only counts for a subject with no
 * directory row at all: another person's identity can list the viewer's
 * address as a linked account, and treating that as "self" would render the
 * viewer's own sessions under someone else's name.
 */
export function useIsSelf(identity: IdentityModel): boolean {
  const { user } = useSession();
  if (identity.userIds.includes(user.id)) return true;
  if (identity.userIds.length > 0) return false;
  return identity.emails.some(
    (email) => email.toLowerCase() === user.email.toLowerCase(),
  );
}

export function useCanReadOthersChats(): boolean {
  const { hasScope } = useRBAC();
  return hasScope("chat:read");
}

export function useIdentityChats(
  identity: IdentityModel,
  from: Date,
  to: Date,
  limit = 5,
): ReturnType<typeof useListChats> {
  const { slug: gramProject } = useIdentityProject();
  // Without chat:read the endpoint returns the caller's own sessions whatever
  // filter it is handed — which is exactly right when the subject IS the
  // caller, and a misattribution for anyone else.
  const hasChatRead = useCanReadOthersChats();
  const isSelf = useIsSelf(identity);
  const canReadOthersChats = hasChatRead || isSelf;
  const userId = identity.userIds[0];
  const externalUserId = identity.externalUserIds[0];
  return useListChats(
    {
      ...(userId ? { userId } : {}),
      ...(userId ? {} : externalUserId ? { externalUserId } : {}),
      from,
      to,
      limit,
      gramProject,
    },
    undefined,
    {
      ...OFF,
      enabled: canReadOthersChats && (!!userId || !!externalUserId),
    },
  );
}

export function useIdentityAuditLogs(
  identity: IdentityModel,
  from: Date,
  to: Date,
): ReturnType<typeof useAuditLogs> {
  const actorId = identity.userIds[0];
  return useAuditLogs({ actorId, from, to }, undefined, {
    ...OFF,
    enabled: !!actorId,
  });
}

export function useIdentityRisk(
  identity: IdentityModel,
  from: Date,
  to: Date,
): ReturnType<typeof useRiskUserBreakdown> {
  const { slug: gramProject } = useIdentityProject();
  const canReadRisk = useCanReadRisk();
  const externalUserId = identity.externalUserIds[0];
  return useRiskUserBreakdown(
    { externalUserId: externalUserId ?? "", from, to, gramProject },
    undefined,
    {
      ...OFF,
      enabled: canReadRisk && !!externalUserId,
    },
  );
}

/**
 * The org member row for this identity, matched on the Gram user id and then
 * on any address the subject is known by. It carries the canonical principal
 * URN and the role ids, neither of which the resolver returns.
 */
export function useIdentityMember(identity: IdentityModel): {
  member: AccessMember | undefined;
  query: ReturnType<typeof useMembers>;
} {
  const membersQuery = useMembers(undefined, undefined, OFF);
  const userIds = new Set(identity.userIds);
  const emails = new Set(identity.emails.map((e) => e.toLowerCase()));
  const members = membersQuery.data?.members ?? [];
  // The directory id settles it outright. An address does not: an
  // unattributed subject carries whatever address an agent reported, and more
  // than one member can claim it — an alias, a shared mailbox, a rotated
  // account. Taking the first of several would attach a real person's roles
  // and challenges to an identity that may not be theirs, so an ambiguous
  // address resolves to nobody and the panels say the identity matches no
  // member row.
  const byUserId = members.find((m) => userIds.has(m.id));
  if (byUserId) return { member: byUserId, query: membersQuery };
  const byEmail = members.filter((m) => emails.has(m.email.toLowerCase()));
  return {
    member: byEmail.length === 1 ? byEmail[0] : undefined,
    query: membersQuery,
  };
}

/**
 * Challenges key on the principal URN the authz engine recorded, which the
 * member row states outright. Without a member row the URN has to be rebuilt,
 * and it is built from the GRAM user id: the engine resolves principals from
 * `authCtx.UserID`, which auth sets to the Gram id rather than the WorkOS one.
 * Falling back to the WorkOS id matches no recorded challenge, and the panel
 * then reports a clean history for someone who may not have one.
 */
export function useIdentityChallenges(
  identity: IdentityModel,
  from: Date,
  to: Date,
): ReturnType<typeof useChallenges> {
  const { member } = useIdentityMember(identity);
  const fallback = identity.userIds[0];
  const principalUrn =
    member?.principalUrn ?? (fallback ? `user:${fallback}` : "");
  return useChallenges({ principalUrn, limit: 25, from, to }, undefined, {
    ...OFF,
    enabled: !!principalUrn,
  });
}

export function useIdentityShadowServers(
  identity: IdentityModel,
  from: Date,
  to: Date,
  limit = 10,
): ReturnType<typeof useShadowMCPInventoryServersForUser> {
  const project = useIdentityProject();
  const canReadRisk = useCanReadRisk();
  // Shadow MCP attributes usage to whatever the client reported, which is an
  // address for some agents and an agent-side id for others — pass both sets.
  // The endpoint caps the list at MAX_USER_KEYS and rejects anything longer,
  // and a rejected request renders as an empty panel — "no shadow servers" —
  // which is the one answer we would not have earned. Duplicates go first (an
  // address is commonly both an email and the reported id), then the
  // remainder is bounded so a subject with an unusual number of aliases still
  // gets the reading for the identifiers we could carry.
  const userKeys = [
    ...new Set([...identity.emails, ...identity.externalUserIds]),
  ].slice(0, MAX_USER_KEYS);
  return useShadowMCPInventoryServersForUser(
    { projectId: project.id, userKeys, from, to, limit },
    undefined,
    { ...OFF, enabled: canReadRisk && userKeys.length > 0 },
  );
}

export function useIdentityDevices(
  identity: IdentityModel,
): ReturnType<typeof useManagedDevices> {
  const userIds = identity.userIds;
  const userEmails = identity.emails;
  return useManagedDevices({ userIds, userEmails, limit: 50 }, undefined, {
    ...OFF,
    enabled: userIds.length > 0 || userEmails.length > 0,
  });
}

/**
 * This identity's row in the windowed org-wide roster, plus its peers.
 *
 * One request serves three readings the per-user summary cannot give: where
 * their figures sit against everyone else's, which agent surfaces they work
 * through (hookSources), and whether any linked account is a personal one.
 * The per-user endpoint returns none of those.
 */
export function useIdentityPeers(
  identity: IdentityModel,
  from: Date,
  to: Date,
): {
  peers: UserSummary[];
  self: UserSummary | undefined;
  isPending: boolean;
  isError: boolean;
  refetch: () => void;
} {
  const client = useGramContext();
  const organization = useOrganization();
  const { slug: projectSlug } = useIdentityProject();
  const query = useQuery({
    queryKey: identityPeersQueryKey(organization.id, projectSlug, from, to),
    queryFn: () => fetchIdentityPeers(client, projectSlug, from, to),
    throwOnError: false,
  });

  const identifiers = new Set(
    [...identity.emails, ...identity.externalUserIds, ...identity.userIds].map(
      (value) => value.toLowerCase(),
    ),
  );
  const peers = query.data ?? [];

  return {
    peers,
    self: peers.find(
      (summary) =>
        identifiers.has((summary.userEmail || "").toLowerCase()) ||
        identifiers.has((summary.userId || "").toLowerCase()),
    ),
    isPending: query.isPending,
    isError: query.isError,
    refetch: () => void query.refetch(),
  };
}
