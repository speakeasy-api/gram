import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListSessionsRequest, ListSessionsSecurity } from "../models/operations/listsessions.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListSessionsQuery, ListSessionsQueryData, prefetchListSessions, queryKeyListSessions } from "./listSessions.core.js";
export { buildListSessionsQuery, type ListSessionsQueryData, prefetchListSessions, queryKeyListSessions, };
export type ListSessionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listSessions telemetry
 *
 * @remarks
 * Org-scoped list of individual chat sessions for a slice of usage, filtered by the same allowlisted dimensions as telemetry.query. Returns per-session cost, token, and tool metrics with cursor pagination.
 */
export declare function useListSessions(request: ListSessionsRequest, security?: ListSessionsSecurity | undefined, options?: QueryHookOptions<ListSessionsQueryData, ListSessionsQueryError>): UseQueryResult<ListSessionsQueryData, ListSessionsQueryError>;
/**
 * listSessions telemetry
 *
 * @remarks
 * Org-scoped list of individual chat sessions for a slice of usage, filtered by the same allowlisted dimensions as telemetry.query. Returns per-session cost, token, and tool metrics with cursor pagination.
 */
export declare function useListSessionsSuspense(request: ListSessionsRequest, security?: ListSessionsSecurity | undefined, options?: SuspenseQueryHookOptions<ListSessionsQueryData, ListSessionsQueryError>): UseSuspenseQueryResult<ListSessionsQueryData, ListSessionsQueryError>;
export declare function setListSessionsData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: ListSessionsQueryData): ListSessionsQueryData | undefined;
export declare function invalidateListSessions(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListSessions(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listSessions.d.ts.map