/**
 * What the readiness read answers, as the GRW-112 contract defines it.
 *
 * Declared here rather than imported from the SDK because the RPC that serves
 * it — identityProviders.getGuidedReadiness — is still being written. When it
 * lands, this file and use-guided-readiness.ts are the only two that change,
 * and every surface that draws readiness keeps reading the same shape.
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
