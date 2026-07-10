import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetProjectMetricsSummaryRequest, GetProjectMetricsSummarySecurity } from "../models/operations/getprojectmetricssummary.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetProjectMetricsSummaryQuery, GetProjectMetricsSummaryQueryData, prefetchGetProjectMetricsSummary, queryKeyGetProjectMetricsSummary } from "./getProjectMetricsSummary.core.js";
export { buildGetProjectMetricsSummaryQuery, type GetProjectMetricsSummaryQueryData, prefetchGetProjectMetricsSummary, queryKeyGetProjectMetricsSummary, };
export type GetProjectMetricsSummaryQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getProjectMetricsSummary telemetry
 *
 * @remarks
 * Get aggregated metrics summary for an entire project
 */
export declare function useGetProjectMetricsSummary(request: GetProjectMetricsSummaryRequest, security?: GetProjectMetricsSummarySecurity | undefined, options?: QueryHookOptions<GetProjectMetricsSummaryQueryData, GetProjectMetricsSummaryQueryError>): UseQueryResult<GetProjectMetricsSummaryQueryData, GetProjectMetricsSummaryQueryError>;
/**
 * getProjectMetricsSummary telemetry
 *
 * @remarks
 * Get aggregated metrics summary for an entire project
 */
export declare function useGetProjectMetricsSummarySuspense(request: GetProjectMetricsSummaryRequest, security?: GetProjectMetricsSummarySecurity | undefined, options?: SuspenseQueryHookOptions<GetProjectMetricsSummaryQueryData, GetProjectMetricsSummaryQueryError>): UseSuspenseQueryResult<GetProjectMetricsSummaryQueryData, GetProjectMetricsSummaryQueryError>;
export declare function setGetProjectMetricsSummaryData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetProjectMetricsSummaryQueryData): GetProjectMetricsSummaryQueryData | undefined;
export declare function invalidateGetProjectMetricsSummary(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetProjectMetricsSummary(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getProjectMetricsSummary.d.ts.map