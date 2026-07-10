import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListChallengeBucketsRequest, ListChallengeBucketsSecurity, Outcome } from "../models/operations/listchallengebuckets.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildChallengeBucketsQuery, ChallengeBucketsQueryData, prefetchChallengeBuckets, queryKeyChallengeBuckets } from "./challengeBuckets.core.js";
export { buildChallengeBucketsQuery, type ChallengeBucketsQueryData, prefetchChallengeBuckets, queryKeyChallengeBuckets, };
export type ChallengeBucketsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listChallengeBuckets access
 *
 * @remarks
 * List authz challenges grouped into time-based burst buckets. Consecutive challenges with the same dimensions within a 10-minute window are collapsed into a single bucket.
 */
export declare function useChallengeBuckets(request?: ListChallengeBucketsRequest | undefined, security?: ListChallengeBucketsSecurity | undefined, options?: QueryHookOptions<ChallengeBucketsQueryData, ChallengeBucketsQueryError>): UseQueryResult<ChallengeBucketsQueryData, ChallengeBucketsQueryError>;
/**
 * listChallengeBuckets access
 *
 * @remarks
 * List authz challenges grouped into time-based burst buckets. Consecutive challenges with the same dimensions within a 10-minute window are collapsed into a single bucket.
 */
export declare function useChallengeBucketsSuspense(request?: ListChallengeBucketsRequest | undefined, security?: ListChallengeBucketsSecurity | undefined, options?: SuspenseQueryHookOptions<ChallengeBucketsQueryData, ChallengeBucketsQueryError>): UseSuspenseQueryResult<ChallengeBucketsQueryData, ChallengeBucketsQueryError>;
export declare function setChallengeBucketsData(client: QueryClient, queryKeyBase: [
    parameters: {
        outcome?: Outcome | undefined;
        principalUrn?: string | undefined;
        scope?: string | undefined;
        projectId?: string | undefined;
        resolved?: boolean | undefined;
        limit?: number | undefined;
        offset?: number | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
], data: ChallengeBucketsQueryData): ChallengeBucketsQueryData | undefined;
export declare function invalidateChallengeBuckets(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        outcome?: Outcome | undefined;
        principalUrn?: string | undefined;
        scope?: string | undefined;
        projectId?: string | undefined;
        resolved?: boolean | undefined;
        limit?: number | undefined;
        offset?: number | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllChallengeBuckets(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=challengeBuckets.d.ts.map