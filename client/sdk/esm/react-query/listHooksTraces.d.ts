import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListHooksTracesRequest, ListHooksTracesSecurity } from "../models/operations/listhookstraces.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListHooksTracesQuery, ListHooksTracesQueryData, prefetchListHooksTraces, queryKeyListHooksTraces } from "./listHooksTraces.core.js";
export { buildListHooksTracesQuery, type ListHooksTracesQueryData, prefetchListHooksTraces, queryKeyListHooksTraces, };
export type ListHooksTracesQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listHooksTraces telemetry
 *
 * @remarks
 * List hook traces aggregated by trace_id with user information
 */
export declare function useListHooksTraces(request: ListHooksTracesRequest, security?: ListHooksTracesSecurity | undefined, options?: QueryHookOptions<ListHooksTracesQueryData, ListHooksTracesQueryError>): UseQueryResult<ListHooksTracesQueryData, ListHooksTracesQueryError>;
/**
 * listHooksTraces telemetry
 *
 * @remarks
 * List hook traces aggregated by trace_id with user information
 */
export declare function useListHooksTracesSuspense(request: ListHooksTracesRequest, security?: ListHooksTracesSecurity | undefined, options?: SuspenseQueryHookOptions<ListHooksTracesQueryData, ListHooksTracesQueryError>): UseSuspenseQueryResult<ListHooksTracesQueryData, ListHooksTracesQueryError>;
export declare function setListHooksTracesData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListHooksTracesQueryData): ListHooksTracesQueryData | undefined;
export declare function invalidateListHooksTraces(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListHooksTraces(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listHooksTraces.d.ts.map