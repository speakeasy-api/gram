import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetToolUsageSummaryRequest, GetToolUsageSummarySecurity } from "../models/operations/gettoolusagesummary.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetToolUsageSummaryQuery, GetToolUsageSummaryQueryData, prefetchGetToolUsageSummary, queryKeyGetToolUsageSummary } from "./getToolUsageSummary.core.js";
export { buildGetToolUsageSummaryQuery, type GetToolUsageSummaryQueryData, prefetchGetToolUsageSummary, queryKeyGetToolUsageSummary, };
export type GetToolUsageSummaryQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getToolUsageSummary telemetry
 *
 * @remarks
 * Get target-aware MCP and tool usage metrics
 */
export declare function useGetToolUsageSummary(request: GetToolUsageSummaryRequest, security?: GetToolUsageSummarySecurity | undefined, options?: QueryHookOptions<GetToolUsageSummaryQueryData, GetToolUsageSummaryQueryError>): UseQueryResult<GetToolUsageSummaryQueryData, GetToolUsageSummaryQueryError>;
/**
 * getToolUsageSummary telemetry
 *
 * @remarks
 * Get target-aware MCP and tool usage metrics
 */
export declare function useGetToolUsageSummarySuspense(request: GetToolUsageSummaryRequest, security?: GetToolUsageSummarySecurity | undefined, options?: SuspenseQueryHookOptions<GetToolUsageSummaryQueryData, GetToolUsageSummaryQueryError>): UseSuspenseQueryResult<GetToolUsageSummaryQueryData, GetToolUsageSummaryQueryError>;
export declare function setGetToolUsageSummaryData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetToolUsageSummaryQueryData): GetToolUsageSummaryQueryData | undefined;
export declare function invalidateGetToolUsageSummary(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetToolUsageSummary(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getToolUsageSummary.d.ts.map