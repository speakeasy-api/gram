/**
 * What the readiness read answers, as the GRW-112 contract defines it, and the
 * one seam between the admin readiness panel and the server.
 *
 * The admin method that will serve it — admin.getOrganizationGuidedReadiness —
 * is still being written, so the hook below reports the read as still in
 * flight, which is what the panel already draws for a slow one. These pages
 * read the admin API through the hand-written `gramAdminFetch`, so the wire
 * shape is snake_case JSON: `toGuidedReadiness` maps it into the shape the
 * panel draws, and repointing the hook at a real query that calls it is the
 * only edit the panel needs. The dashboard carries the same panel shape for
 * the same reason, in the SDK's own casing, in
 * client/dashboard/src/components/guided-readiness/types.ts; the two answer one
 * server type and should be repointed together.
 */

/** Who has to act on a check that did not pass. */
export type GuidedReadinessOwner = "platform_admin" | "customer" | "speakeasy";

export interface GuidedReadinessCheck {
  /** Stable identifier, e.g. the one a staff member quotes in a handoff. */
  key: string;
  ok: boolean;
  /** One sentence, safe to show a customer. */
  detail: string;
  /** One sentence for staff: what to do, and where. May be empty. */
  remedy: string;
  owner: GuidedReadinessOwner;
  checkedAt: Date;
}

export interface GuidedReadiness {
  provider: string;
  /** The advanced flow is offered. True only when every check passed. */
  eligible: boolean;
  checks: GuidedReadinessCheck[];
  checkedAt: Date;
}

/** The admin API's own JSON, as `gramAdminFetch` hands it back. */
export interface GuidedReadinessCheckResponse {
  key: string;
  ok: boolean;
  detail: string;
  remedy: string;
  owner: GuidedReadinessOwner;
  checked_at: string;
}

export interface GuidedReadinessResponse {
  provider: string;
  eligible: boolean;
  checks: GuidedReadinessCheckResponse[];
  checked_at: string;
}

export function toGuidedReadiness(
  response: GuidedReadinessResponse,
): GuidedReadiness {
  return {
    provider: response.provider,
    eligible: response.eligible,
    checkedAt: new Date(response.checked_at),
    checks: response.checks.map((check) => ({
      key: check.key,
      ok: check.ok,
      detail: check.detail,
      remedy: check.remedy,
      owner: check.owner,
      checkedAt: new Date(check.checked_at),
    })),
  };
}

/** The parts of a React Query result the readiness panel draws. */
export interface GuidedReadinessQuery {
  data: GuidedReadiness | undefined;
  isPending: boolean;
  isFetching: boolean;
  error: unknown;
  refetch: () => void;
}

export function useOrganizationGuidedReadiness(
  organizationID: string,
): GuidedReadinessQuery {
  // The real query gates on this the same way: no organization, no read.
  const enabled = organizationID !== "";
  return {
    data: undefined,
    isPending: enabled,
    isFetching: false,
    error: null,
    refetch: () => undefined,
  };
}
