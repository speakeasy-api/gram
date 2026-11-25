import { useMemo } from "react";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import { useMembers } from "@gram/client/react-query/members.js";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";

export type AuditPrincipal = {
  kind: "user" | "agent";
  id: string;
  urn: string;
};

export function auditPrincipalLabel(
  principal: AuditPrincipal | null,
  identities: ReturnType<typeof useAuditPrincipals>,
  fallback: string,
): string {
  if (!principal) return fallback;
  if (principal.kind === "agent")
    return identities.agents.get(principal.id)?.name ?? principal.urn;
  const user = identities.users.get(principal.id);
  return user?.name || user?.email || fallback;
}

export function parseAuditPrincipal(value: unknown): AuditPrincipal | null {
  if (typeof value !== "string") return null;
  const match = /^(user|agent):([^\s]+)$/.exec(value);
  return match
    ? { kind: match[1] as "user" | "agent", id: match[2]!, urn: value }
    : null;
}

export function auditSubjectPrincipal(log: AuditLog): AuditPrincipal | null {
  // Session IDs identify credentials, not owners. The actor is the revoker.
  if (log.subjectType === "user_session") {
    return (
      parseAuditPrincipal(log.metadata?.["subject_urn"]) ??
      parseAuditPrincipal(log.subjectDisplayName)
    );
  }
  if (log.subjectType === "agent" || log.subjectType === "user") {
    return parseAuditPrincipal(
      log.subjectId.startsWith(`${log.subjectType}:`)
        ? log.subjectId
        : `${log.subjectType}:${log.subjectId}`,
    );
  }
  return null;
}

export function auditActorPrincipal(log: AuditLog): AuditPrincipal | null {
  if (log.actorType !== "agent" && log.actorType !== "user") return null;
  if (
    log.actorType === "user" &&
    ["system", "user:system"].includes(log.actorId)
  )
    return null;
  return parseAuditPrincipal(
    log.actorId.startsWith(`${log.actorType}:`)
      ? log.actorId
      : `${log.actorType}:${log.actorId}`,
  );
}

/** One cached inventory request per kind for the whole feed, never per row. */
export function useAuditPrincipals(logs: AuditLog[]): {
  agents: Map<string, ManagedAgent>;
  users: Map<string, AccessMember>;
} {
  const principals = useMemo(
    () =>
      logs.flatMap((log) => [
        auditSubjectPrincipal(log),
        auditActorPrincipal(log),
      ]),
    [logs],
  );
  const agents = useReadableAgents(principals.some((p) => p?.kind === "agent"));
  const members = useMembers(undefined, undefined, {
    enabled: principals.some((p) => p?.kind === "user"),
    throwOnError: false,
    retry: false,
  });
  return useMemo(
    () => ({
      agents: new Map(
        (agents.isError ? [] : (agents.data ?? [])).map((agent) => [
          agent.id,
          agent,
        ]),
      ),
      users: new Map(
        (members.isError ? [] : (members.data?.members ?? [])).map((member) => [
          member.id,
          member,
        ]),
      ),
    }),
    [agents.data, agents.isError, members.data, members.isError],
  );
}
