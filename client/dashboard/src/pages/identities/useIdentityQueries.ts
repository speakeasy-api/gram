import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import { fetchIdentityRoster, identityRosterQueryKey } from "./identityRoster";
import { useSearchParams } from "react-router";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import { useAuditLogs } from "@gram/client/react-query/auditLogs.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useManagedDevices } from "@gram/client/react-query/managedDevices.js";
import { useGetUserMetricsSummary } from "@gram/client/react-query/getUserMetricsSummary.js";
import { useRiskUserBreakdown } from "@gram/client/react-query/riskUserBreakdown.js";
import { useShadowMCPInventoryServersForUser } from "@gram/client/react-query/shadowMCPInventoryServersForUser.js";

const OFF = { throwOnError: false } as const;

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
 * The page is org-level, but usage, chats, cost and risk are recorded per
 * project, so the project is a filter here rather than a route segment. It
 * lives in the URL so a link carries the slice the sender was looking at, and
 * falls back to the project the reader most recently worked in.
 */
export function useIdentityProject(): {
  slug: string;
  id: string;
  setSlug: (slug: string) => void;
  options: { slug: string; name: string }[];
} {
  const organization = useOrganization();
  const currentProject = useProject();
  const [searchParams, setSearchParams] = useSearchParams();
  const options = organization.projects.map((project) => ({
    slug: project.slug,
    name: project.name,
  }));
  const fromUrl = searchParams.get("project");
  const slug =
    (fromUrl && options.some((option) => option.slug === fromUrl)
      ? fromUrl
      : "") ||
    currentProject.slug ||
    options[0]?.slug ||
    "";

  return {
    slug,
    id:
      organization.projects.find((project) => project.slug === slug)?.id ?? "",
    options,
    setSlug: (next) =>
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          params.set("project", next);
          return params;
        },
        { replace: true },
      ),
  };
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
  const { slug: gramProject } = useIdentityProject();
  const userId = identity.userIds[0];
  const externalUserId = identity.externalUserIds[0];
  return useGetUserMetricsSummary(
    {
      gramProject,
      getUserMetricsSummaryPayload: {
        from,
        to,
        ...(userId ? { userId } : externalUserId ? { externalUserId } : {}),
      },
    },
    undefined,
    { ...OFF, enabled: !!userId || !!externalUserId },
  );
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
} {
  const client = useGramContext();
  const organization = useOrganization();
  const projectSlug = useProjectSlugForRequests();
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
    queryFn: () => fetchIdentityRoster(client),
    throwOnError: false,
    enabled: !!identity && !hasDirectoryRow && identifiers.size > 0,
  });

  if (!identity) return { known: true, isPending: true };
  if (hasDirectoryRow) return { known: true, isPending: false };
  // Only an unattributed subject is the "identifier nothing was recorded
  // under" case: an api-key or agent identity legitimately carries neither an
  // address nor an agent id, and is still a real subject.
  if (identity.kind !== "unattributed")
    return { known: true, isPending: false };
  if (identifiers.size === 0) return { known: false, isPending: false };
  if (query.isPending) return { known: false, isPending: true };
  return {
    known: (query.data ?? []).some((summary) =>
      identifiers.has((summary.userEmail || summary.userId).toLowerCase()),
    ),
    isPending: false,
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
): ReturnType<typeof useAuditLogs> {
  const actorId = identity.userIds[0];
  return useAuditLogs({ actorId }, undefined, { ...OFF, enabled: !!actorId });
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
  const member = membersQuery.data?.members.find(
    (m) => userIds.has(m.id) || emails.has(m.email.toLowerCase()),
  );
  return { member, query: membersQuery };
}

/**
 * Challenges key on the principal URN the authz engine recorded, which the
 * member row states outright; the WorkOS user id is the fallback for a subject
 * with no member row, since RBAC assignments are held against that user.
 */
export function useIdentityChallenges(
  identity: IdentityModel,
): ReturnType<typeof useChallenges> {
  const { member } = useIdentityMember(identity);
  const fallback = identity.workosUserId ?? identity.userIds[0];
  const principalUrn =
    member?.principalUrn ?? (fallback ? `user:${fallback}` : "");
  return useChallenges({ principalUrn, limit: 25 }, undefined, {
    ...OFF,
    enabled: !!principalUrn,
  });
}

export function useIdentityShadowServers(
  identity: IdentityModel,
  limit = 10,
): ReturnType<typeof useShadowMCPInventoryServersForUser> {
  const project = useIdentityProject();
  const canReadRisk = useCanReadRisk();
  // Shadow MCP attributes usage to whatever the client reported, which is an
  // address for some agents and an agent-side id for others — pass both sets.
  const userKeys = [...identity.emails, ...identity.externalUserIds];
  return useShadowMCPInventoryServersForUser(
    { projectId: project.id, userKeys, limit },
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
