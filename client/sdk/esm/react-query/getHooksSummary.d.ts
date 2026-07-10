import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetHooksSummaryRequest, GetHooksSummarySecurity } from "../models/operations/gethookssummary.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetHooksSummaryQuery, GetHooksSummaryQueryData, prefetchGetHooksSummary, queryKeyGetHooksSummary } from "./getHooksSummary.core.js";
export { buildGetHooksSummaryQuery, type GetHooksSummaryQueryData, prefetchGetHooksSummary, queryKeyGetHooksSummary, };
export type GetHooksSummaryQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getHooksSummary telemetry
 *
 * @remarks
 * Get aggregated hooks metrics grouped by server
 */
export declare function useGetHooksSummary(request: GetHooksSummaryRequest, security?: GetHooksSummarySecurity | undefined, options?: QueryHookOptions<GetHooksSummaryQueryData, GetHooksSummaryQueryError>): UseQueryResult<GetHooksSummaryQueryData, GetHooksSummaryQueryError>;
/**
 * getHooksSummary telemetry
 *
 * @remarks
 * Get aggregated hooks metrics grouped by server
 */
export declare function useGetHooksSummarySuspense(request: GetHooksSummaryRequest, security?: GetHooksSummarySecurity | undefined, options?: SuspenseQueryHookOptions<GetHooksSummaryQueryData, GetHooksSummaryQueryError>): UseSuspenseQueryResult<GetHooksSummaryQueryData, GetHooksSummaryQueryError>;
export declare function setGetHooksSummaryData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetHooksSummaryQueryData): GetHooksSummaryQueryData | undefined;
export declare function invalidateGetHooksSummary(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetHooksSummary(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getHooksSummary.d.ts.map