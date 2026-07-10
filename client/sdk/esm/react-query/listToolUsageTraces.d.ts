import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListToolUsageTracesRequest, ListToolUsageTracesSecurity } from "../models/operations/listtoolusagetraces.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListToolUsageTracesQuery, ListToolUsageTracesQueryData, prefetchListToolUsageTraces, queryKeyListToolUsageTraces } from "./listToolUsageTraces.core.js";
export { buildListToolUsageTracesQuery, type ListToolUsageTracesQueryData, prefetchListToolUsageTraces, queryKeyListToolUsageTraces, };
export type ListToolUsageTracesQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listToolUsageTraces telemetry
 *
 * @remarks
 * List target-aware MCP and tool usage traces
 */
export declare function useListToolUsageTraces(request: ListToolUsageTracesRequest, security?: ListToolUsageTracesSecurity | undefined, options?: QueryHookOptions<ListToolUsageTracesQueryData, ListToolUsageTracesQueryError>): UseQueryResult<ListToolUsageTracesQueryData, ListToolUsageTracesQueryError>;
/**
 * listToolUsageTraces telemetry
 *
 * @remarks
 * List target-aware MCP and tool usage traces
 */
export declare function useListToolUsageTracesSuspense(request: ListToolUsageTracesRequest, security?: ListToolUsageTracesSecurity | undefined, options?: SuspenseQueryHookOptions<ListToolUsageTracesQueryData, ListToolUsageTracesQueryError>): UseSuspenseQueryResult<ListToolUsageTracesQueryData, ListToolUsageTracesQueryError>;
export declare function setListToolUsageTracesData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListToolUsageTracesQueryData): ListToolUsageTracesQueryData | undefined;
export declare function invalidateListToolUsageTraces(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListToolUsageTraces(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listToolUsageTraces.d.ts.map