import { useProject, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import type { Chat } from "@gram/client/models/components/chat.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useMemo } from "react";
import type { FacetGroup, FacetValue } from "./LogsFacetRail";

/**
 * How much of Tool Logs the viewer sees. Every member reads the rows of their
 * own sessions and of the agents they own; log:read on the project widens that
 * to every member's rows. Mirrors the server's own-scope fallback.
 */
export type ToolLogsAccess = {
  /** project:read on the active project, which the Tool Logs page admits. */
  canOpen: boolean;
  /** log:read on the active project: every member's rows, not only their own. */
  projectWide: boolean;
  /**
   * Grants still loading. Both flags are false until they arrive, so a reader
   * that acts on them fails closed rather than asking for everyone's rows.
   */
  isLoading: boolean;
};

export function useToolLogsAccess(): ToolLogsAccess {
  const project = useProject();
  const { hasScope, isLoading } = useRBAC();
  // An empty id would make this an "any project" check, so outside a project
  // the answer is no.
  const canOpen = Boolean(project.id) && hasScope("project:read", project.id);
  const projectWide = Boolean(project.id) && hasScope("log:read", project.id);
  return useMemo(
    () => ({ canOpen, projectWide, isLoading }),
    [canOpen, projectWide, isLoading],
  );
}

/**
 * Whether the viewer owns this agent's logs. A latched agent (owner
 * reassignment pending) is nobody's, as on the server.
 */
export function isOwnedAgent(
  agent: Pick<ManagedAgent, "ownerUserId" | "ownerReassignmentRequiredAt">,
  viewerUserId: string,
): boolean {
  return (
    agent.ownerUserId === viewerUserId && !agent.ownerReassignmentRequiredAt
  );
}

/** The agents whose logs the viewer owns, out of the agents they can read. */
export function ownedAgentIds(
  agents: readonly ManagedAgent[] | undefined,
  viewerUserId: string,
): ReadonlySet<string> {
  return new Set(
    (agents ?? [])
      .filter((agent) => isOwnedAgent(agent, viewerUserId))
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
 * The Tool Logs facets for a viewer limited to their own rows. No project-wide
 * options are fetched for them, so a group holds only what a link selected
 * (kept so it can be cleared) plus, for the identity facets, the viewer and
 * the agents they own. Roles enumerate other members, so that facet goes, and
 * a group left with nothing to pick goes with it.
 */
export function ownScopeFacetGroups(
  groups: readonly FacetGroup[],
  own: { user: readonly FacetValue[]; agent: readonly FacetValue[] },
): FacetGroup[] {
  return groups.flatMap((group) => {
    if (group.id === "role") return [];
    let values: FacetValue[] = group.values.map(
      ({ count: _count, ...value }) => value,
    );
    if (group.id === "user" || group.id === "agent") {
      const ownValues = own[group.id];
      const offered = new Set(ownValues.map((value) => value.value));
      values = [
        ...ownValues,
        ...values.filter((value) => !offered.has(value.value)),
      ];
    }
    return values.length > 0 ? [{ ...group, values }] : [];
  });
}
