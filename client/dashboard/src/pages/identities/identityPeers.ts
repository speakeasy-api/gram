import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import { Metrics } from "@gram/client/models/components/searchuserspayload.js";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { unwrapAsync } from "@gram/client/types/fp";

/**
 * The roster for one window, so a rank reads against the same period as the
 * figure it qualifies. The all-time roster (identityRoster) answers "has this
 * identity ever been seen"; ranking a week's spend against an all-time peer
 * set would put a number and its context on different clocks.
 */
export function identityPeersQueryKey(
  organizationId: string,
  projectSlug: string,
  from: Date,
  to: Date,
): (string | number)[] {
  return [
    "identities",
    "peers",
    organizationId,
    projectSlug,
    from.getTime(),
    to.getTime(),
  ];
}

export async function fetchIdentityPeers(
  client: Parameters<typeof telemetrySearchUsers>[0],
  gramProject: string,
  from: Date,
  to: Date,
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;

  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        gramProject,
        searchUsersPayload: {
          cursor,
          filter: { from, to },
          limit: 1000,
          sort: "desc",
          userType: "internal",
          // Deliberately NOT Source.AgentMetrics, which the all-time roster
          // uses: that view carries token counts but reports zero cost, no
          // chat counts, no hook sources and no linked accounts — every
          // reading this hook exists to serve.
          metrics: Metrics.Full,
        },
      }),
    );

    users.push(...result.users);
    cursor = result.nextCursor;
  } while (cursor);

  return users;
}

/**
 * Where a value sits in its peer group: the 1-based position among peers that
 * recorded anything at all, and the group's median.
 *
 * Zero-valued peers are excluded from both. "2nd of 9" counts the nine people
 * who actually spent something; including everyone the directory knows would
 * make a rank improve whenever a colleague went on holiday.
 */
export type PeerStanding = {
  rank: number;
  total: number;
  median: number;
};

export function peerStanding(
  values: number[],
  value: number,
): PeerStanding | undefined {
  const active = values.filter((v) => v > 0).sort((a, b) => b - a);
  if (value <= 0 || active.length < 2) return undefined;

  const rank = active.findIndex((v) => v <= value) + 1;
  const mid = Math.floor(active.length / 2);
  const median =
    active.length % 2 === 0
      ? ((active[mid - 1] ?? 0) + (active[mid] ?? 0)) / 2
      : (active[mid] ?? 0);

  return { rank: rank || active.length, total: active.length, median };
}

/** e.g. "2nd of 9 · 3.1x median". */
export function standingLabel(standing: PeerStanding, value: number): string {
  const suffix =
    standing.rank % 10 === 1 && standing.rank % 100 !== 11
      ? "st"
      : standing.rank % 10 === 2 && standing.rank % 100 !== 12
        ? "nd"
        : standing.rank % 10 === 3 && standing.rank % 100 !== 13
          ? "rd"
          : "th";
  const base = `${standing.rank}${suffix} of ${standing.total}`;
  if (standing.median <= 0) return base;
  const ratio = value / standing.median;
  // Within a rounding step of the median, "1.0x median" is noise.
  if (ratio >= 0.95 && ratio <= 1.05) return `${base} · at median`;
  return `${base} · ${ratio.toFixed(1)}x median`;
}
