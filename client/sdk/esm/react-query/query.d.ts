import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildQueryQuery, prefetchQuery, queryKeyQuery, QueryQueryData } from "./query.core.js";
export { buildQueryQuery, prefetchQuery, queryKeyQuery, type QueryQueryData };
export type QueryQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * query telemetry
 *
 * @remarks
 * Generic, org-scoped analytics query over pre-aggregated usage metrics. Returns both a grouped table and a per-group hourly timeseries for the same slice of data, supporting arbitrary allowlisted group-by dimensions and filters (e.g. group by department_name, then drill in by filtering department_name and grouping by role).
 */
export declare function useQuery(request: operations.QueryRequest, security?: operations.QuerySecurity | undefined, options?: QueryHookOptions<QueryQueryData, QueryQueryError>): UseQueryResult<QueryQueryData, QueryQueryError>;
/**
 * query telemetry
 *
 * @remarks
 * Generic, org-scoped analytics query over pre-aggregated usage metrics. Returns both a grouped table and a per-group hourly timeseries for the same slice of data, supporting arbitrary allowlisted group-by dimensions and filters (e.g. group by department_name, then drill in by filtering department_name and grouping by role).
 */
export declare function useQuerySuspense(request: operations.QueryRequest, security?: operations.QuerySecurity | undefined, options?: SuspenseQueryHookOptions<QueryQueryData, QueryQueryError>): UseSuspenseQueryResult<QueryQueryData, QueryQueryError>;
export declare function setQueryData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: QueryQueryData): QueryQueryData | undefined;
export declare function invalidateQuery(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllQuery(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=query.d.ts.map