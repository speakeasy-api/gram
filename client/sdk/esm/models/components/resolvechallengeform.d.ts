import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * How the challenge is being resolved.
 */
export declare const ResolveChallengeFormResolutionType: {
  readonly RoleAssigned: "role_assigned";
  readonly Dismissed: "dismissed";
};
/**
 * How the challenge is being resolved.
 */
export type ResolveChallengeFormResolutionType = ClosedEnum<
  typeof ResolveChallengeFormResolutionType
>;
export type ResolveChallengeForm = {
  /**
   * IDs of the challenges in ClickHouse to resolve.
   */
  challengeIds: Array<string>;
  /**
   * Principal that was denied.
   */
  principalUrn: string;
  /**
   * How the challenge is being resolved.
   */
  resolutionType: ResolveChallengeFormResolutionType;
  /**
   * Resource ID from the challenge.
   */
  resourceId?: string | undefined;
  /**
   * Resource kind from the challenge.
   */
  resourceKind?: string | undefined;
  /**
   * Role slug to assign (required when resolution_type=role_assigned).
   */
  roleSlug?: string | undefined;
  /**
   * Scope that was denied.
   */
  scope: string;
};
/** @internal */
export declare const ResolveChallengeFormResolutionType$outboundSchema: z.ZodMiniEnum<
  typeof ResolveChallengeFormResolutionType
>;
/** @internal */
export type ResolveChallengeForm$Outbound = {
  challenge_ids: Array<string>;
  principal_urn: string;
  resolution_type: string;
  resource_id?: string | undefined;
  resource_kind?: string | undefined;
  role_slug?: string | undefined;
  scope: string;
};
/** @internal */
export declare const ResolveChallengeForm$outboundSchema: z.ZodMiniType<
  ResolveChallengeForm$Outbound,
  ResolveChallengeForm
>;
export declare function resolveChallengeFormToJSON(
  resolveChallengeForm: ResolveChallengeForm,
): string;
//# sourceMappingURL=resolvechallengeform.d.ts.map
