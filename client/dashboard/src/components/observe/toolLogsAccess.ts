import { useProject, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import type { Chat } from "@gram/client/models/components/chat.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useMemo } from "react";
import type { FacetGroup } from "./LogsFacetRail";

/**
 * How much of Tool Logs the viewer sees. Every member reads the rows of their
 * own sessions and of the agents they own; log:read on the project widens that
 * to every member's rows. Mirrors the server's own-scope fallback.
 */
export type ToolLogsAccess = {
  /** log:read on the active project: every member's rows, not only their own. */
  projectWide: boolean;
  /** Grants still loading; projectWide is false until they arrive. */
  isLoading: boolean;
};

export function useToolLogsAccess(): ToolLogsAccess {
  const project = useProject();
  const { hasScope, isLoading } = useRBAC();
  // An empty id would make this an "any project" check, so outside a project
  // the answer is no.
  const projectWide = Boolean(project.id) && hasScope("log:read", project.id);
  return useMemo(() => ({ projectWide, isLoading }), [projectWide, isLoading]);
}

/**
 * The agents whose logs the viewer owns, out of the agents they can read. A
 * latched agent (owner reassignment pending) is nobody's, as on the server.
 */
export function ownedAgentIds(
  agents: readonly ManagedAgent[] | undefined,
  viewerUserId: string,
): ReadonlySet<string> {
  return new Set(
    (agents ?? [])
      .filter(
        (agent) =>
          agent.ownerUserId === viewerUserId &&
          !agent.ownerReassignmentRequiredAt,
      )
      .map((agent) => agent.id),
  );
}

export function useOwnedAgentIds(enabled: boolean): ReadonlySet<string> {
  const { user } = useSession();
  const agents = useReadableAgents(enabled);
  const list = agents.isError ? undefined : agents.data;
  return useMemo(() => ownedAgentIds(list, user.id), [list, user.id]);
}

/**
 * Whether the viewer reads this session's tool calls without log:read: it is
 * their own, or it was driven by an agent they own.
 */
export function isOwnScopeSession(
  chat: Pick<Chat, "userId" | "agentId"> | undefined,
  viewerUserId: string,
  owned: ReadonlySet<string>,
): boolean {
  if (!chat) return false;
  if (chat.userId && chat.userId === viewerUserId) return true;
  return Boolean(chat.agentId && owned.has(chat.agentId));
}

/**
 * The Tool Logs facets for a viewer limited to their own rows. The facet
 * counts are project-wide aggregates, so they are dropped rather than promise
 * rows the list will not show, and identity facets offer only the viewer and
 * the agents they own. Roles enumerate other members, so that facet goes.
 */
export function ownScopeFacetGroups(
  groups: readonly FacetGroup[],
  viewerEmail: string,
  owned: ReadonlySet<string>,
): FacetGroup[] {
  return groups.flatMap((group) => {
    if (group.id === "role") return [];
    let values = group.values;
    if (group.id === "user") {
      values = values.filter((value) => value.value === viewerEmail);
    } else if (group.id === "agent") {
      values = values.filter((value) => owned.has(value.value));
    }
    return [
      {
        ...group,
        values: values.map(({ count: _count, ...value }) => value),
      },
    ];
  });
}
