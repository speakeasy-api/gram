import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";

export type FleetSource = "agent" | "assistant" | "session";
export type FleetRow = {
  id: string;
  source: FleetSource;
  title: string;
  subtitle: string;
  ownerId?: string;
  createdById?: string;
  captureUserId?: string;
  externalCaptureUserId?: string;
  personId?: string;
  personRole: "Owner" | "Creator" | "Captured user" | "Captured external user";
  personName: string;
  department: string;
  actingIdentity: string;
  lastActivity?: Date;
  agent?: ManagedAgent;
  assistant?: Assistant;
  session?: ChatOverview;
};

export const sourceLabels: Record<FleetSource, string> = {
  agent: "Registered agents",
  assistant: "Assistants",
  session: "Captured sessions",
};

/** Only explicit IDs join namespaces. A user/owner or matching name proves no agent binding. */
export function buildFleetRows({
  agents,
  assistants,
  sessions,
  members,
  projectId,
  observedAssistantActivity,
}: {
  agents: ManagedAgent[];
  assistants: Assistant[];
  sessions: ChatOverview[];
  members: AccessMember[];
  projectId: string;
  observedAssistantActivity?: ReadonlyMap<string, Date>;
}): FleetRow[] {
  const people = new Map(members.map((member) => [member.id, member]));
  const assistantById = new Map(
    assistants.map((assistant) => [assistant.id, assistant]),
  );
  const assistantActivity = new Map(observedAssistantActivity);
  for (const session of sessions) {
    if (!session.assistantId) continue;
    const previous = assistantActivity.get(session.assistantId);
    if (!previous || session.lastMessageTimestamp > previous)
      assistantActivity.set(session.assistantId, session.lastMessageTimestamp);
  }
  const owner = (id?: string, fallback?: string) => ({
    personId: id,
    personName:
      people.get(id ?? "")?.name ||
      fallback ||
      (id ? "Directory profile unavailable" : "Unknown attribution"),
    department: people.get(id ?? "")?.department || "Unassigned department",
  });
  return [
    ...agents
      .filter((agent) => !agent.projectId || agent.projectId === projectId)
      .map((agent): FleetRow => ({
        id: `agent:${agent.id}`,
        source: "agent",
        title: agent.name,
        subtitle: agent.projectId
          ? "Registered in this project"
          : "Organization-wide agent",
        ownerId: agent.ownerUserId,
        personRole: "Owner",
        ...owner(agent.ownerUserId, agent.ownerProfile?.displayName),
        actingIdentity: `agent:${agent.id}`,
        lastActivity: agent.lastCredentialUsedAt,
        agent,
      })),
    ...assistants
      .filter((assistant) => assistant.projectId === projectId)
      .map((assistant): FleetRow => ({
        id: `assistant:${assistant.id}`,
        source: "assistant",
        title: assistant.name,
        subtitle: "Gram Assistant",
        createdById: assistant.createdByUserId,
        personRole: "Creator",
        ...owner(assistant.createdByUserId),
        actingIdentity: `assistant:${assistant.id}`,
        assistant,
        lastActivity: assistantActivity.get(assistant.id),
      })),
    ...sessions.map((session): FleetRow => {
      const assistant = session.assistantId
        ? assistantById.get(session.assistantId)
        : undefined;
      return {
        id: `session:${session.id}`,
        source: "session",
        title: session.title || "Untitled session",
        subtitle:
          session.assistantName ||
          session.originatingClient ||
          session.source ||
          "Unknown source",
        captureUserId: session.userId,
        externalCaptureUserId: session.externalUserId,
        personRole:
          !session.userId && session.externalUserId
            ? "Captured external user"
            : "Captured user",
        ...owner(
          session.userId,
          !session.userId && session.externalUserId
            ? `External user (unverified) · ${session.externalUserId}`
            : undefined,
        ),
        actingIdentity: session.assistantId
          ? `assistant:${session.assistantId}`
          : "Not established by capture",
        lastActivity: session.lastMessageTimestamp,
        session,
        assistant,
      };
    }),
  ];
}

export const FLEET_WINDOW_MS = 24 * 60 * 60 * 1_000;

/** Both views use observed timestamps; profile creation/edits are not activity. */
export function recentFleetRows(rows: FleetRow[], now: number): FleetRow[] {
  return rows.filter((row) => {
    const timestamp = row.lastActivity?.getTime();
    return timestamp != null && timestamp >= now - FLEET_WINDOW_MS;
  });
}

export type FleetDepartment = {
  name: string;
  identities: { id: string; name: string; roles: string[]; rows: FleetRow[] }[];
};
export function fleetDirectory(rows: FleetRow[]): FleetDepartment[] {
  const departments = new Map<
    string,
    Map<string, { id: string; name: string; roles: string[]; rows: FleetRow[] }>
  >();
  for (const row of rows) {
    let people = departments.get(row.department);
    if (!people) {
      people = new Map();
      departments.set(row.department, people);
    }
    // Unknown owners remain separate instead of collapsing unrelated sessions into one identity.
    const key = row.personId
      ? `user:${row.personId}`
      : row.externalCaptureUserId
        ? `external:${row.externalCaptureUserId}`
        : `unknown:${row.id}`;
    let person = people.get(key);
    if (!person) {
      person = { id: key, name: row.personName, roles: [], rows: [] };
      people.set(key, person);
    }
    if (!person.roles.includes(row.personRole))
      person.roles.push(row.personRole);
    person.rows.push(row);
  }
  return [...departments]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, people]) => ({ name, identities: [...people.values()] }));
}

export function agentRestrictionLabel(
  id: string,
  agents: ManagedAgent[],
  inventoryAvailable: boolean,
): string {
  const agent = agents.find((item) => item.id === id);
  if (agent) return agent.name;
  return `${inventoryAvailable ? "Deleted or unavailable agent" : "Agent"} · ${id.slice(0, 8)}`;
}
