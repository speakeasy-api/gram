import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { QueryRequest, QuerySecurity } from "../models/operations/query.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildTelemetryQueryQuery, prefetchTelemetryQuery, queryKeyTelemetryQuery, TelemetryQueryQueryData } from "./telemetryQuery.core.js";
export { buildTelemetryQueryQuery, prefetchTelemetryQuery, queryKeyTelemetryQuery, type TelemetryQueryQueryData, };
export type TelemetryQueryQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * query telemetry
 *
 * @remarks
 * Generic, org-scoped analytics query over pre-aggregated usage metrics. Returns both a grouped table and a per-group hourly timeseries for the same slice of data, supporting arbitrary allowlisted group-by dimensions and filters (e.g. group by department_name, then drill in by filtering department_name and grouping by role).
 */
export declare function useTelemetryQuery(request: QueryRequest, security?: QuerySecurity | undefined, options?: QueryHookOptions<TelemetryQueryQueryData, TelemetryQueryQueryError>): UseQueryResult<TelemetryQueryQueryData, TelemetryQueryQueryError>;
/**
 * query telemetry
 *
 * @remarks
 * Generic, org-scoped analytics query over pre-aggregated usage metrics. Returns both a grouped table and a per-group hourly timeseries for the same slice of data, supporting arbitrary allowlisted group-by dimensions and filters (e.g. group by department_name, then drill in by filtering department_name and grouping by role).
 */
export declare function useTelemetryQuerySuspense(request: QueryRequest, security?: QuerySecurity | undefined, options?: SuspenseQueryHookOptions<TelemetryQueryQueryData, TelemetryQueryQueryError>): UseSuspenseQueryResult<TelemetryQueryQueryData, TelemetryQueryQueryError>;
export declare function setTelemetryQueryData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: TelemetryQueryQueryData): TelemetryQueryQueryData | undefined;
export declare function invalidateTelemetryQuery(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllTelemetryQuery(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=telemetryQuery.d.ts.map