import { deviceIntegrationsListManagedDevices } from "@gram/client/funcs/deviceIntegrationsListManagedDevices";
import { unwrapAsync } from "@gram/client/types/fp";

/**
 * The MDM fleet, folded to one coverage bucket per identity.
 *
 * The devices endpoint answers per user id or email, which is a question the
 * detail page asks for one person. The roster asks it of everyone at once, so
 * this reads the fleet whole and indexes it by both keys — a device carries a
 * resolved Gram user id only when its MDM-reported email matched a member, so
 * neither key alone reaches every row.
 */

/** Page size the endpoint allows, and the number of pages we will walk. */
const DEVICE_PAGE_SIZE = 200;
const MAX_DEVICE_PAGES = 10;

/**
 * Buckets from best to worst. An identity with several machines is described
 * by their best one: someone whose laptop reports in is covered, whatever a
 * retired second machine says.
 */
const BUCKET_RANK = [
  "agent_active",
  "agent_other_device",
  "agent_stale",
  "no_agent",
  "unresolved_email",
  "no_email",
  "missing",
] as const;

/** The bucket for an identity MDM holds no device for at all. */
export const NO_DEVICE_BUCKET = "no_device";

export type DeviceCoverageIndex = {
  /** Best bucket per Gram user id. */
  byUserId: Map<string, string>;
  /** Best bucket per MDM-reported assigned email, lowercased. */
  byEmail: Map<string, string>;
  /** Devices read. Zero means no MDM inventory to filter against. */
  deviceCount: number;
  /** True when the fleet is larger than the pages we walked. */
  truncated: boolean;
};

export function deviceCoverageQueryKey(organizationId: string): string[] {
  return ["identities", "device-coverage", organizationId];
}

function betterBucket(current: string | undefined, next: string): string {
  if (current === undefined) return next;
  const currentRank = BUCKET_RANK.indexOf(current as (typeof BUCKET_RANK)[0]);
  const nextRank = BUCKET_RANK.indexOf(next as (typeof BUCKET_RANK)[0]);
  // An unranked bucket (one the API grew after this list) loses to a known
  // one rather than shadowing it.
  if (nextRank === -1) return current;
  if (currentRank === -1) return next;
  return nextRank < currentRank ? next : current;
}

export async function fetchDeviceCoverage(
  client: Parameters<typeof deviceIntegrationsListManagedDevices>[0],
): Promise<DeviceCoverageIndex> {
  const byUserId = new Map<string, string>();
  const byEmail = new Map<string, string>();
  let deviceCount = 0;
  let cursor: string | undefined;
  let pages = 0;

  do {
    const { result } = await unwrapAsync(
      deviceIntegrationsListManagedDevices(client, {
        cursor,
        limit: DEVICE_PAGE_SIZE,
      }),
    );
    for (const device of result.devices) {
      deviceCount += 1;
      const bucket = device.coverageBucket;
      if (device.userId) {
        byUserId.set(
          device.userId,
          betterBucket(byUserId.get(device.userId), bucket),
        );
      }
      const email = (device.userEmail ?? "").toLowerCase();
      if (email) {
        byEmail.set(email, betterBucket(byEmail.get(email), bucket));
      }
    }
    cursor = result.nextCursor;
    pages += 1;
  } while (cursor && pages < MAX_DEVICE_PAGES);

  return { byUserId, byEmail, deviceCount, truncated: !!cursor };
}

/**
 * The bucket describing one identity, or {@link NO_DEVICE_BUCKET} when the
 * inventory holds no machine for either of their keys.
 */
export function coverageForIdentity(
  index: DeviceCoverageIndex | undefined,
  identity: { id: string; email: string },
): string {
  if (!index) return NO_DEVICE_BUCKET;
  // Unattributed usage rows carry a synthetic "usage:"-prefixed id that no
  // device can match; their email is the only key worth trying.
  const byId = index.byUserId.get(identity.id);
  const byEmail = index.byEmail.get(identity.email.toLowerCase());
  if (byId && byEmail) return betterBucket(byId, byEmail);
  return byId ?? byEmail ?? NO_DEVICE_BUCKET;
}
