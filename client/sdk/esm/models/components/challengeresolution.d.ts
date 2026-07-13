import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const ChallengeResolutionResolutionType: {
  readonly RoleAssigned: "role_assigned";
  readonly Dismissed: "dismissed";
};
export type ChallengeResolutionResolutionType = ClosedEnum<
  typeof ChallengeResolutionResolutionType
>;
export type ChallengeResolution = {
  /**
   * ClickHouse challenge ID.
   */
  challengeId: string;
  createdAt: Date;
  /**
   * Resolution record ID.
   */
  id: string;
  /**
   * Organization ID.
   */
  organizationId: string;
  /**
   * Denied principal.
   */
  principalUrn: string;
  resolutionType: ChallengeResolutionResolutionType;
  /**
   * Admin who resolved.
   */
  resolvedBy: string;
  /**
   * Resource ID.
   */
  resourceId?: string | undefined;
  /**
   * Resource kind.
   */
  resourceKind?: string | undefined;
  /**
   * Assigned role slug.
   */
  roleSlug?: string | undefined;
  /**
   * Denied scope.
   */
  scope: string;
};
/** @internal */
export declare const ChallengeResolutionResolutionType$inboundSchema: z.ZodMiniEnum<
  typeof ChallengeResolutionResolutionType
>;
/** @internal */
export declare const ChallengeResolution$inboundSchema: z.ZodMiniType<
  ChallengeResolution,
  unknown
>;
export declare function challengeResolutionFromJSON(
  jsonString: string,
): SafeParseResult<ChallengeResolution, SDKValidationError>;
//# sourceMappingURL=challengeresolution.d.ts.map
