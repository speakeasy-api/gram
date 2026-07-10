import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const Operation: {
  readonly Require: "require";
  readonly RequireAny: "require_any";
  readonly Filter: "filter";
};
export type Operation = ClosedEnum<typeof Operation>;
export declare const Outcome: {
  readonly Allow: "allow";
  readonly Deny: "deny";
  readonly Error: "error";
};
export type Outcome = ClosedEnum<typeof Outcome>;
/**
 * Kind of principal.
 */
export declare const PrincipalType: {
  readonly User: "user";
  readonly ApiKey: "api_key";
  readonly Assistant: "assistant";
};
/**
 * Kind of principal.
 */
export type PrincipalType = ClosedEnum<typeof PrincipalType>;
export declare const Reason: {
  readonly GrantMatched: "grant_matched";
  readonly NoGrants: "no_grants";
  readonly ScopeUnsatisfied: "scope_unsatisfied";
  readonly DenyGrant: "deny_grant";
  readonly InvalidCheck: "invalid_check";
  readonly RbacSkippedApikey: "rbac_skipped_apikey";
  readonly DevOverride: "dev_override";
};
export type Reason = ClosedEnum<typeof Reason>;
/**
 * How the bucket was resolved.
 */
export declare const ResolutionType: {
  readonly RoleAssigned: "role_assigned";
  readonly Dismissed: "dismissed";
};
/**
 * How the bucket was resolved.
 */
export type ResolutionType = ClosedEnum<typeof ResolutionType>;
/**
 * A group of consecutive challenges with the same dimensions that occurred within a 10-minute window.
 */
export type ChallengeBucket = {
  /**
   * Number of individual challenges in this bucket.
   */
  challengeCount: number;
  /**
   * IDs of all challenges in this bucket.
   */
  challengeIds: Array<string>;
  /**
   * Total grants evaluated.
   */
  evaluatedGrantCount: number;
  /**
   * Timestamp of the earliest challenge in the bucket.
   */
  firstSeen: Date;
  /**
   * ID of the most recent challenge in the bucket.
   */
  id: string;
  /**
   * Timestamp of the most recent challenge in the bucket.
   */
  lastSeen: Date;
  /**
   * Number of grants that matched.
   */
  matchedGrantCount: number;
  operation: Operation;
  /**
   * Organization the principal was acting in.
   */
  organizationId: string;
  outcome: Outcome;
  /**
   * User avatar URL when available.
   */
  photoUrl?: string | undefined;
  /**
   * Kind of principal.
   */
  principalType: PrincipalType;
  /**
   * Principal URN e.g. user:<uuid> or api_key:<id>.
   */
  principalUrn: string;
  /**
   * Project scope (empty for org-level checks).
   */
  projectId?: string | undefined;
  reason: Reason;
  /**
   * Role slug assigned (when resolution_type=role_assigned).
   */
  resolutionRoleSlug?: string | undefined;
  /**
   * How the bucket was resolved.
   */
  resolutionType?: ResolutionType | undefined;
  /**
   * When the bucket was resolved by an admin.
   */
  resolvedAt?: Date | undefined;
  /**
   * URN of the admin who resolved.
   */
  resolvedBy?: string | undefined;
  /**
   * Resource ID of the check.
   */
  resourceId?: string | undefined;
  /**
   * Resource kind of the check.
   */
  resourceKind?: string | undefined;
  /**
   * Roles the principal had loaded.
   */
  roleSlugs: Array<string>;
  /**
   * Scope that was checked.
   */
  scope: string;
  /**
   * Email when available.
   */
  userEmail?: string | undefined;
};
/** @internal */
export declare const Operation$inboundSchema: z.ZodMiniEnum<typeof Operation>;
/** @internal */
export declare const Outcome$inboundSchema: z.ZodMiniEnum<typeof Outcome>;
/** @internal */
export declare const PrincipalType$inboundSchema: z.ZodMiniEnum<
  typeof PrincipalType
>;
/** @internal */
export declare const Reason$inboundSchema: z.ZodMiniEnum<typeof Reason>;
/** @internal */
export declare const ResolutionType$inboundSchema: z.ZodMiniEnum<
  typeof ResolutionType
>;
/** @internal */
export declare const ChallengeBucket$inboundSchema: z.ZodMiniType<
  ChallengeBucket,
  unknown
>;
export declare function challengeBucketFromJSON(
  jsonString: string,
): SafeParseResult<ChallengeBucket, SDKValidationError>;
//# sourceMappingURL=challengebucket.d.ts.map
