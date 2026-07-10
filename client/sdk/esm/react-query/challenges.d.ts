import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  ListChallengesRequest,
  ListChallengesSecurity,
  QueryParamOutcome,
} from "../models/operations/listchallenges.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildChallengesQuery,
  ChallengesQueryData,
  prefetchChallenges,
  queryKeyChallenges,
} from "./challenges.core.js";
export {
  buildChallengesQuery,
  type ChallengesQueryData,
  prefetchChallenges,
  queryKeyChallenges,
};
export type ChallengesQueryError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * listChallenges access
 *
 * @remarks
 * List authz challenge events from ClickHouse, enriched with resolution state from PostgreSQL.
 */
export declare function useChallenges(
  request?: ListChallengesRequest | undefined,
  security?: ListChallengesSecurity | undefined,
  options?: QueryHookOptions<ChallengesQueryData, ChallengesQueryError>,
): UseQueryResult<ChallengesQueryData, ChallengesQueryError>;
/**
 * listChallenges access
 *
 * @remarks
 * List authz challenge events from ClickHouse, enriched with resolution state from PostgreSQL.
 */
export declare function useChallengesSuspense(
  request?: ListChallengesRequest | undefined,
  security?: ListChallengesSecurity | undefined,
  options?: SuspenseQueryHookOptions<ChallengesQueryData, ChallengesQueryError>,
): UseSuspenseQueryResult<ChallengesQueryData, ChallengesQueryError>;
export declare function setChallengesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      outcome?: QueryParamOutcome | undefined;
      principalUrn?: string | undefined;
      scope?: string | undefined;
      projectId?: string | undefined;
      resolved?: boolean | undefined;
      ids?: Array<string> | undefined;
      limit?: number | undefined;
      offset?: number | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: ChallengesQueryData,
): ChallengesQueryData | undefined;
export declare function invalidateChallenges(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        outcome?: QueryParamOutcome | undefined;
        principalUrn?: string | undefined;
        scope?: string | undefined;
        projectId?: string | undefined;
        resolved?: boolean | undefined;
        ids?: Array<string> | undefined;
        limit?: number | undefined;
        offset?: number | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllChallenges(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=challenges.d.ts.map
