import { InfiniteData, InvalidateQueryFilters, QueryClient, UseInfiniteQueryResult, UseQueryResult, UseSuspenseInfiniteQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRemoteSessionsRequest, ListRemoteSessionsSecurity } from "../models/operations/listremotesessions.js";
import { InfiniteQueryHookOptions, QueryHookOptions, SuspenseInfiniteQueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRemoteSessionsInfiniteQuery, buildRemoteSessionsQuery, prefetchRemoteSessions, prefetchRemoteSessionsInfinite, queryKeyRemoteSessions, queryKeyRemoteSessionsInfinite, RemoteSessionsInfiniteQueryData, RemoteSessionsPageParams, RemoteSessionsQueryData } from "./remoteSessions.core.js";
export { buildRemoteSessionsInfiniteQuery, buildRemoteSessionsQuery, prefetchRemoteSessions, prefetchRemoteSessionsInfinite, queryKeyRemoteSessions, queryKeyRemoteSessionsInfinite, type RemoteSessionsInfiniteQueryData, type RemoteSessionsPageParams, type RemoteSessionsQueryData, };
export type RemoteSessionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listRemoteSessions remoteSessions
 *
 * @remarks
 * List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).
 */
export declare function useRemoteSessions(request?: ListRemoteSessionsRequest | undefined, security?: ListRemoteSessionsSecurity | undefined, options?: QueryHookOptions<RemoteSessionsQueryData, RemoteSessionsQueryError>): UseQueryResult<RemoteSessionsQueryData, RemoteSessionsQueryError>;
/**
 * listRemoteSessions remoteSessions
 *
 * @remarks
 * List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).
 */
export declare function useRemoteSessionsSuspense(request?: ListRemoteSessionsRequest | undefined, security?: ListRemoteSessionsSecurity | undefined, options?: SuspenseQueryHookOptions<RemoteSessionsQueryData, RemoteSessionsQueryError>): UseSuspenseQueryResult<RemoteSessionsQueryData, RemoteSessionsQueryError>;
/**
 * listRemoteSessions remoteSessions
 *
 * @remarks
 * List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).
 */
export declare function useRemoteSessionsInfinite(request?: ListRemoteSessionsRequest | undefined, security?: ListRemoteSessionsSecurity | undefined, options?: InfiniteQueryHookOptions<RemoteSessionsInfiniteQueryData, RemoteSessionsQueryError>): UseInfiniteQueryResult<InfiniteData<RemoteSessionsInfiniteQueryData, RemoteSessionsPageParams>, RemoteSessionsQueryError>;
/**
 * listRemoteSessions remoteSessions
 *
 * @remarks
 * List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).
 */
export declare function useRemoteSessionsInfiniteSuspense(request?: ListRemoteSessionsRequest | undefined, security?: ListRemoteSessionsSecurity | undefined, options?: SuspenseInfiniteQueryHookOptions<RemoteSessionsInfiniteQueryData, RemoteSessionsQueryError>): UseSuspenseInfiniteQueryResult<InfiniteData<RemoteSessionsInfiniteQueryData, RemoteSessionsPageParams>, RemoteSessionsQueryError>;
export declare function setRemoteSessionsData(client: QueryClient, queryKeyBase: [
    parameters: {
        subjectUrn?: string | undefined;
        remoteSessionClientId?: string | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RemoteSessionsQueryData): RemoteSessionsQueryData | undefined;
export declare function invalidateRemoteSessions(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        subjectUrn?: string | undefined;
        remoteSessionClientId?: string | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRemoteSessions(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=remoteSessions.d.ts.map