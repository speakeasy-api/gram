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
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;

  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
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
