import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { useProject } from "@/contexts/Auth";
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
 * Telemetry keys usage on either the Gram user id or the id an agent reported,
 * and the endpoint takes exactly one of them, so prefer the directory user and
 * fall back to the agent identifier for subjects with no directory row.
 */
export function useIdentityMetrics(
  identity: IdentityModel,
  from: Date,
  to: Date,
): ReturnType<typeof useGetUserMetricsSummary> {
  const userId = identity.userIds[0];
  const externalUserId = identity.externalUserIds[0];
  return useGetUserMetricsSummary(
    {
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

export function useIdentityChats(
  identity: IdentityModel,
  from: Date,
  to: Date,
  limit = 5,
): ReturnType<typeof useListChats> {
  const userId = identity.userIds[0];
  const externalUserId = identity.externalUserIds[0];
  return useListChats(
    {
      ...(userId ? { userId } : {}),
      ...(userId ? {} : externalUserId ? { externalUserId } : {}),
      from,
      to,
      limit,
    },
    undefined,
    { ...OFF, enabled: !!userId || !!externalUserId },
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
  const externalUserId = identity.externalUserIds[0];
  return useRiskUserBreakdown(
    { externalUserId: externalUserId ?? "", from, to },
    undefined,
    {
      ...OFF,
      enabled: !!externalUserId,
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
  const project = useProject();
  // Shadow MCP attributes usage to whatever the client reported, which is an
  // address for some agents and an agent-side id for others — pass both sets.
  const userKeys = [...identity.emails, ...identity.externalUserIds];
  return useShadowMCPInventoryServersForUser(
    { projectId: project.id, userKeys, limit },
    undefined,
    { ...OFF, enabled: userKeys.length > 0 },
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
