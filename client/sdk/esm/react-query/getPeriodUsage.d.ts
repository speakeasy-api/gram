import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetPeriodUsageRequest, GetPeriodUsageSecurity } from "../models/operations/getperiodusage.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetPeriodUsageQuery, GetPeriodUsageQueryData, prefetchGetPeriodUsage, queryKeyGetPeriodUsage } from "./getPeriodUsage.core.js";
export { buildGetPeriodUsageQuery, type GetPeriodUsageQueryData, prefetchGetPeriodUsage, queryKeyGetPeriodUsage, };
export type GetPeriodUsageQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getPeriodUsage usage
 *
 * @remarks
 * Get the usage for an organization for a given period
 */
export declare function useGetPeriodUsage(request?: GetPeriodUsageRequest | undefined, security?: GetPeriodUsageSecurity | undefined, options?: QueryHookOptions<GetPeriodUsageQueryData, GetPeriodUsageQueryError>): UseQueryResult<GetPeriodUsageQueryData, GetPeriodUsageQueryError>;
/**
 * getPeriodUsage usage
 *
 * @remarks
 * Get the usage for an organization for a given period
 */
export declare function useGetPeriodUsageSuspense(request?: GetPeriodUsageRequest | undefined, security?: GetPeriodUsageSecurity | undefined, options?: SuspenseQueryHookOptions<GetPeriodUsageQueryData, GetPeriodUsageQueryError>): UseSuspenseQueryResult<GetPeriodUsageQueryData, GetPeriodUsageQueryError>;
export declare function setGetPeriodUsageData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: GetPeriodUsageQueryData): GetPeriodUsageQueryData | undefined;
export declare function invalidateGetPeriodUsage(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetPeriodUsage(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getPeriodUsage.d.ts.map