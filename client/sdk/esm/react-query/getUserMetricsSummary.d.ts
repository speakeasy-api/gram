import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetUserMetricsSummaryRequest, GetUserMetricsSummarySecurity } from "../models/operations/getusermetricssummary.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetUserMetricsSummaryQuery, GetUserMetricsSummaryQueryData, prefetchGetUserMetricsSummary, queryKeyGetUserMetricsSummary } from "./getUserMetricsSummary.core.js";
export { buildGetUserMetricsSummaryQuery, type GetUserMetricsSummaryQueryData, prefetchGetUserMetricsSummary, queryKeyGetUserMetricsSummary, };
export type GetUserMetricsSummaryQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getUserMetricsSummary telemetry
 *
 * @remarks
 * Get aggregated metrics summary grouped by user
 */
export declare function useGetUserMetricsSummary(request: GetUserMetricsSummaryRequest, security?: GetUserMetricsSummarySecurity | undefined, options?: QueryHookOptions<GetUserMetricsSummaryQueryData, GetUserMetricsSummaryQueryError>): UseQueryResult<GetUserMetricsSummaryQueryData, GetUserMetricsSummaryQueryError>;
/**
 * getUserMetricsSummary telemetry
 *
 * @remarks
 * Get aggregated metrics summary grouped by user
 */
export declare function useGetUserMetricsSummarySuspense(request: GetUserMetricsSummaryRequest, security?: GetUserMetricsSummarySecurity | undefined, options?: SuspenseQueryHookOptions<GetUserMetricsSummaryQueryData, GetUserMetricsSummaryQueryError>): UseSuspenseQueryResult<GetUserMetricsSummaryQueryData, GetUserMetricsSummaryQueryError>;
export declare function setGetUserMetricsSummaryData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetUserMetricsSummaryQueryData): GetUserMetricsSummaryQueryData | undefined;
export declare function invalidateGetUserMetricsSummary(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetUserMetricsSummary(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getUserMetricsSummary.d.ts.map