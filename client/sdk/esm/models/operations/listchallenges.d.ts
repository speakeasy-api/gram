import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListChallengesSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
/**
 * Filter by outcome.
 */
export declare const QueryParamOutcome: {
  readonly Allow: "allow";
  readonly Deny: "deny";
};
/**
 * Filter by outcome.
 */
export type QueryParamOutcome = ClosedEnum<typeof QueryParamOutcome>;
export type ListChallengesRequest = {
  /**
   * Filter by outcome.
   */
  outcome?: QueryParamOutcome | undefined;
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
   * Fetch specific challenges by ID. When set, other filters and pagination are ignored.
   */
  ids?: Array<string> | undefined;
  /**
   * Maximum number of results to return.
   */
  limit?: number | undefined;
  /**
   * Number of results to skip.
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
export type ListChallengesSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListChallengesSecurity$outboundSchema: z.ZodMiniType<
  ListChallengesSecurity$Outbound,
  ListChallengesSecurity
>;
export declare function listChallengesSecurityToJSON(
  listChallengesSecurity: ListChallengesSecurity,
): string;
/** @internal */
export declare const QueryParamOutcome$outboundSchema: z.ZodMiniEnum<
  typeof QueryParamOutcome
>;
/** @internal */
export type ListChallengesRequest$Outbound = {
  outcome?: string | undefined;
  principal_urn?: string | undefined;
  scope?: string | undefined;
  project_id?: string | undefined;
  resolved?: boolean | undefined;
  ids?: Array<string> | undefined;
  limit: number;
  offset: number;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListChallengesRequest$outboundSchema: z.ZodMiniType<
  ListChallengesRequest$Outbound,
  ListChallengesRequest
>;
export declare function listChallengesRequestToJSON(
  listChallengesRequest: ListChallengesRequest,
): string;
//# sourceMappingURL=listchallenges.d.ts.map
