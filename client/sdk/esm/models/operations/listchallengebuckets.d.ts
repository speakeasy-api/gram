import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListChallengeBucketsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
/**
 * Filter by outcome.
 */
export declare const Outcome: {
  readonly Allow: "allow";
  readonly Deny: "deny";
};
/**
 * Filter by outcome.
 */
export type Outcome = ClosedEnum<typeof Outcome>;
export type ListChallengeBucketsRequest = {
  /**
   * Filter by outcome.
   */
  outcome?: Outcome | undefined;
  /**
   * Filter by principal URN.
   */
  principalUrn?: string | undefined;
  /**
   * Filter by scope.
   */
  scope?: string | undefined;
  /**
   * Filter to a specific project.
   */
  projectId?: string | undefined;
  /**
   * Filter by resolution state. True = only resolved, false = only unresolved.
   */
  resolved?: boolean | undefined;
  /**
   * Maximum number of buckets to return.
   */
  limit?: number | undefined;
  /**
   * Number of buckets to skip.
   */
  offset?: number | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type ListChallengeBucketsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListChallengeBucketsSecurity$outboundSchema: z.ZodMiniType<
  ListChallengeBucketsSecurity$Outbound,
  ListChallengeBucketsSecurity
>;
export declare function listChallengeBucketsSecurityToJSON(
  listChallengeBucketsSecurity: ListChallengeBucketsSecurity,
): string;
/** @internal */
export declare const Outcome$outboundSchema: z.ZodMiniEnum<typeof Outcome>;
/** @internal */
export type ListChallengeBucketsRequest$Outbound = {
  outcome?: string | undefined;
  principal_urn?: string | undefined;
  scope?: string | undefined;
  project_id?: string | undefined;
  resolved?: boolean | undefined;
  limit: number;
  offset: number;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListChallengeBucketsRequest$outboundSchema: z.ZodMiniType<
  ListChallengeBucketsRequest$Outbound,
  ListChallengeBucketsRequest
>;
export declare function listChallengeBucketsRequestToJSON(
  listChallengeBucketsRequest: ListChallengeBucketsRequest,
): string;
//# sourceMappingURL=listchallengebuckets.d.ts.map
