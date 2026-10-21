import type { Employee } from "@/components/observe/insightsEmployeesData";
import { identityHasAccount } from "./identityKind";
import type { Gram } from "@gram/client";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { Lifecycle } from "@gram/client/models/operations/listagents.js";

/** The lifecycle values the agent roster filter may send. */
export type AgentLifecycleFilter = Lifecycle;
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import { Source } from "@gram/client/models/components/searchuserspayload.js";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { unwrapAsync } from "@gram/client/types/fp";

/**
 * Far enough back to be "everything": the agent-usage view has its own
 * retention, so this bound is what asks for all of it rather than what defines
 * it.
 */
const ALL_TIME_FROM = new Date("2020-01-01T00:00:00Z");

/**
 * Keyed by org and by the project the request is made under: telemetry is
 * per-project, so a key naming only the org would serve one project's roster
 * for another.
 */
export function identityRosterQueryKey(
  organizationId: string,
  projectSlug: string,
): string[] {
  return ["identities", "usage", "all-time", organizationId, projectSlug];
}

/**
 * Every identity telemetry has recorded, whether or not the directory knows
 * them. Email-keyed identities come from the pre-aggregated agent-usage view;
 * the ones with no address are surfaced from raw logs by the same endpoint.
 */
export async function fetchIdentityRoster(
  client: Parameters<typeof telemetrySearchUsers>[0],
  /** The project to read under, which must match the cache key's. */
  gramProject: string,
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;

  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        gramProject,
        searchUsersPayload: {
          cursor,
          filter: { from: ALL_TIME_FROM, to: new Date() },
          limit: 1000,
          sort: "desc",
          userType: "internal",
          source: Source.AgentMetrics,
        },
      }),
    );

    users.push(...result.users);
    cursor = result.nextCursor;
  } while (cursor);

  return users;
}

/** One server-ordered page of the agent inventory. */
export type RegisteredAgentPage = {
  items: ManagedAgent[];
  /** Absent once the inventory is exhausted. */
  nextCursor?: string;
};

export type RegisteredAgentPageRequest = {
  cursor?: string;
  limit?: number;
  /** Case-insensitive substring match on the agent name, applied by the server. */
  search?: string;
  /** Name order, which is the only order the inventory offers. */
  sortOrder?: "asc" | "desc";
  /** Lifecycle states to keep; empty means every state. */
  lifecycle?: Lifecycle[];
  /** Owners to keep; empty means every owner. */
  ownerUserIds?: string[];
  /** Registration window, as the server compares it. */
  after?: Date;
  before?: Date;
};

/**
 * Agent management is org-scoped and returns 404 when its rollout is off.
 *
 * The page is decided by the server: ordering, searching and the cursor all
 * belong to the same query, so narrowing or reordering in the browser would
 * describe a different list than the one the cursor walks.
 */
export async function fetchRegisteredAgents(
  client: Gram,
  request: RegisteredAgentPageRequest = {},
  signal?: AbortSignal,
): Promise<RegisteredAgentPage> {
  try {
    const page = await client.agents.list(
      {
        cursor: request.cursor,
        limit: request.limit,
        search: request.search?.trim() || undefined,
        nameOrder: request.sortOrder,
        lifecycle: request.lifecycle?.length ? request.lifecycle : undefined,
        ownerUserIds: request.ownerUserIds?.length
          ? request.ownerUserIds
          : undefined,
        registeredAfter: request.after,
        registeredBefore: request.before,
      },
      undefined,
      { signal },
    );
    return {
      items: page.result.items,
      nextCursor: page.result.nextCursor,
    };
  } catch (error) {
    if (error instanceof GramError && error.statusCode === 404)
      return { items: [] };
    throw error;
  }
}

/** Inventory rows imply no directory profile, membership or telemetry activity. */
export function registeredAgentIdentity(agent: ManagedAgent): Employee {
  return {
    id: `agent:${agent.id}`,
    registeredAgentId: agent.id,
    name: agent.name,
    email: "",
    role: "",
    inDirectory: false,
    seen: false,
    tokenCount: 0,
    lastActivity: "—",
    lastActivityTimestamp: null,
    accounts: [],
    mostRecentAccount: null,
    hasPersonalAccount: false,
    roleIds: [],
    department: "",
    teams: [],
  };
}

/** Preserve roster context while selecting the agent at the management destination. */
export function registeredAgentHref(
  path: string,
  search: string,
  id: string,
): string {
  const params = new URLSearchParams(search);
  params.set("id", id);
  return `${path}?${params.toString()}`;
}

/**
 * The identity-fact filters, each answered on its own: in the directory, a
 * member of this organization, and seen working. Values are the option values
 * offered by the list ("in_directory", "not_in_directory", and so on); an
 * empty or absent value leaves that fact unfiltered.
 */
export type IdentityFactFilters = {
  directory?: string;
  membership?: string;
  seen?: string;
};

/**
 * Inventory-only agents have no known directory, membership or activity
 * classification, so any of these filters leaves them out rather than
 * answering "no" for them.
 */
export function matchesIdentityTelemetryFilters(
  identity: Employee,
  facts: IdentityFactFilters,
  activity: string | undefined,
): boolean {
  const { directory, membership, seen } = facts;
  if (
    identity.registeredAgentId &&
    (directory || membership || seen || activity)
  )
    return false;
  if (directory && identity.inDirectory !== (directory === "in_directory"))
    return false;
  if (membership && identityHasAccount(identity) !== (membership === "member"))
    return false;
  if (seen && identity.seen !== (seen === "seen")) return false;
  return !activity || matchesActivity(identity, activity);
}

const DAY_MS = 24 * 60 * 60 * 1000;

function matchesActivity(identity: Employee, bucket: string): boolean {
  const timestamp = identity.lastActivityTimestamp;
  if (bucket === "never") return timestamp === null;
  if (timestamp === null) return false;
  const age = Date.now() - timestamp;
  if (bucket === "7d") return age <= 7 * DAY_MS;
  if (bucket === "30d") return age <= 30 * DAY_MS;
  if (bucket === "older") return age > 30 * DAY_MS;
  return true;
}
