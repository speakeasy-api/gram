import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const AuthzChallengeOperation: {
  readonly Require: "require";
  readonly RequireAny: "require_any";
  readonly Filter: "filter";
};
export type AuthzChallengeOperation = ClosedEnum<
  typeof AuthzChallengeOperation
>;
export declare const AuthzChallengeOutcome: {
  readonly Allow: "allow";
  readonly Deny: "deny";
  readonly Error: "error";
};
export type AuthzChallengeOutcome = ClosedEnum<typeof AuthzChallengeOutcome>;
/**
 * Kind of principal.
 */
export declare const AuthzChallengePrincipalType: {
  readonly User: "user";
  readonly ApiKey: "api_key";
  readonly Assistant: "assistant";
};
/**
 * Kind of principal.
 */
export type AuthzChallengePrincipalType = ClosedEnum<
  typeof AuthzChallengePrincipalType
>;
export declare const AuthzChallengeReason: {
  readonly GrantMatched: "grant_matched";
  readonly NoGrants: "no_grants";
  readonly ScopeUnsatisfied: "scope_unsatisfied";
  readonly DenyGrant: "deny_grant";
  readonly InvalidCheck: "invalid_check";
  readonly RbacSkippedApikey: "rbac_skipped_apikey";
  readonly DevOverride: "dev_override";
};
export type AuthzChallengeReason = ClosedEnum<typeof AuthzChallengeReason>;
/**
 * How the challenge was resolved.
 */
export declare const AuthzChallengeResolutionType: {
  readonly RoleAssigned: "role_assigned";
  readonly Dismissed: "dismissed";
};
/**
 * How the challenge was resolved.
 */
export type AuthzChallengeResolutionType = ClosedEnum<
  typeof AuthzChallengeResolutionType
>;
export type AuthzChallenge = {
  /**
   * Total grants evaluated.
   */
  evaluatedGrantCount: number;
  /**
   * Unique challenge identifier.
   */
  id: string;
  /**
   * Number of grants that matched.
   */
  matchedGrantCount: number;
  operation: AuthzChallengeOperation;
  /**
   * Organization the principal was acting in.
   */
  organizationId: string;
  outcome: AuthzChallengeOutcome;
  /**
   * User avatar URL when available.
   */
  photoUrl?: string | undefined;
  /**
   * Kind of principal.
   */
  principalType: AuthzChallengePrincipalType;
  /**
   * Principal URN e.g. user:<uuid> or api_key:<id>.
   */
  principalUrn: string;
  /**
   * Project scope (empty for org-level checks).
   */
  projectId?: string | undefined;
  reason: AuthzChallengeReason;
  /**
   * Role slug assigned (when resolution_type=role_assigned).
   */
  resolutionRoleSlug?: string | undefined;
  /**
   * How the challenge was resolved.
   */
  resolutionType?: AuthzChallengeResolutionType | undefined;
  /**
   * When the challenge was resolved by an admin.
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
   * When the authz decision was made.
   */
  timestamp: Date;
  /**
   * Email when available.
   */
  userEmail?: string | undefined;
};
/** @internal */
export declare const AuthzChallengeOperation$inboundSchema: z.ZodMiniEnum<
  typeof AuthzChallengeOperation
>;
/** @internal */
export declare const AuthzChallengeOutcome$inboundSchema: z.ZodMiniEnum<
  typeof AuthzChallengeOutcome
>;
/** @internal */
export declare const AuthzChallengePrincipalType$inboundSchema: z.ZodMiniEnum<
  typeof AuthzChallengePrincipalType
>;
/** @internal */
export declare const AuthzChallengeReason$inboundSchema: z.ZodMiniEnum<
  typeof AuthzChallengeReason
>;
/** @internal */
export declare const AuthzChallengeResolutionType$inboundSchema: z.ZodMiniEnum<
  typeof AuthzChallengeResolutionType
>;
/** @internal */
export declare const AuthzChallenge$inboundSchema: z.ZodMiniType<
  AuthzChallenge,
  unknown
>;
export declare function authzChallengeFromJSON(
  jsonString: string,
): SafeParseResult<AuthzChallenge, SDKValidationError>;
//# sourceMappingURL=authzchallenge.d.ts.map
