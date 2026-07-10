import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetToolUsageFilterOptionsRequest, GetToolUsageFilterOptionsSecurity } from "../models/operations/gettoolusagefilteroptions.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetToolUsageFilterOptionsQuery, GetToolUsageFilterOptionsQueryData, prefetchGetToolUsageFilterOptions, queryKeyGetToolUsageFilterOptions } from "./getToolUsageFilterOptions.core.js";
export { buildGetToolUsageFilterOptionsQuery, type GetToolUsageFilterOptionsQueryData, prefetchGetToolUsageFilterOptions, queryKeyGetToolUsageFilterOptions, };
export type GetToolUsageFilterOptionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getToolUsageFilterOptions telemetry
 *
 * @remarks
 * Get filter options for target-aware MCP and tool usage metrics
 */
export declare function useGetToolUsageFilterOptions(request: GetToolUsageFilterOptionsRequest, security?: GetToolUsageFilterOptionsSecurity | undefined, options?: QueryHookOptions<GetToolUsageFilterOptionsQueryData, GetToolUsageFilterOptionsQueryError>): UseQueryResult<GetToolUsageFilterOptionsQueryData, GetToolUsageFilterOptionsQueryError>;
/**
 * getToolUsageFilterOptions telemetry
 *
 * @remarks
 * Get filter options for target-aware MCP and tool usage metrics
 */
export declare function useGetToolUsageFilterOptionsSuspense(request: GetToolUsageFilterOptionsRequest, security?: GetToolUsageFilterOptionsSecurity | undefined, options?: SuspenseQueryHookOptions<GetToolUsageFilterOptionsQueryData, GetToolUsageFilterOptionsQueryError>): UseSuspenseQueryResult<GetToolUsageFilterOptionsQueryData, GetToolUsageFilterOptionsQueryError>;
export declare function setGetToolUsageFilterOptionsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetToolUsageFilterOptionsQueryData): GetToolUsageFilterOptionsQueryData | undefined;
export declare function invalidateGetToolUsageFilterOptions(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetToolUsageFilterOptions(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getToolUsageFilterOptions.d.ts.map