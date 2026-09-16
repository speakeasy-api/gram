/**
 * What the readiness read answers, as the GRW-112 contract defines it, and the
 * one seam between the admin readiness panel and the server.
 *
 * The admin method that will serve it — admin.getOrganizationGuidedReadiness —
 * is still being written, so nothing is generated for it yet and the hook below
 * reports the read as still in flight, which is what the panel already draws
 * for a slow one. Repointing that body at the generated query is the only edit
 * the panel needs, and it is where whatever the server hands back is mapped
 * into the shape below. The dashboard carries the same shape for the same
 * reason, in client/dashboard/src/components/guided-readiness/types.ts; the two
 * answer one server type and should be repointed together.
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
