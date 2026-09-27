import type { UserSession } from "@gram/client/models/components/usersession.js";

export type ResolvedSession = UserSession & { subjectAgentId?: string };

/** Only resolve agents returned by the authorization-filtered management API. */
export function resolveSessionAgents(
  sessions: UserSession[],
  agents: ReadonlyArray<{ id: string; name: string }>,
): ResolvedSession[] {
  const byUrn = new Map(agents.map((agent) => [`agent:${agent.id}`, agent]));
  return sessions.map((session) => {
    const agent =
      session.subjectType === "agent"
        ? byUrn.get(session.subjectUrn)
        : undefined;
    if (session.subjectType !== "agent") return session;
    return {
      ...session,
      subjectDisplayName: agent?.name,
      subjectAgentId: agent?.id,
    };
  });
}
