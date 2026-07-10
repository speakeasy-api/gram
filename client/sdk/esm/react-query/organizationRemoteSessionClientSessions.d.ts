import { InfiniteData, InvalidateQueryFilters, QueryClient, UseInfiniteQueryResult, UseQueryResult, UseSuspenseInfiniteQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListOrganizationRemoteSessionClientSessionsRequest, ListOrganizationRemoteSessionClientSessionsSecurity } from "../models/operations/listorganizationremotesessionclientsessions.js";
import { InfiniteQueryHookOptions, QueryHookOptions, SuspenseInfiniteQueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildOrganizationRemoteSessionClientSessionsInfiniteQuery, buildOrganizationRemoteSessionClientSessionsQuery, OrganizationRemoteSessionClientSessionsInfiniteQueryData, OrganizationRemoteSessionClientSessionsPageParams, OrganizationRemoteSessionClientSessionsQueryData, prefetchOrganizationRemoteSessionClientSessions, prefetchOrganizationRemoteSessionClientSessionsInfinite, queryKeyOrganizationRemoteSessionClientSessions, queryKeyOrganizationRemoteSessionClientSessionsInfinite } from "./organizationRemoteSessionClientSessions.core.js";
export { buildOrganizationRemoteSessionClientSessionsInfiniteQuery, buildOrganizationRemoteSessionClientSessionsQuery, type OrganizationRemoteSessionClientSessionsInfiniteQueryData, type OrganizationRemoteSessionClientSessionsPageParams, type OrganizationRemoteSessionClientSessionsQueryData, prefetchOrganizationRemoteSessionClientSessions, prefetchOrganizationRemoteSessionClientSessionsInfinite, queryKeyOrganizationRemoteSessionClientSessions, queryKeyOrganizationRemoteSessionClientSessionsInfinite, };
export type OrganizationRemoteSessionClientSessionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listClientSessions organizationRemoteSessions
 *
 * @remarks
 * List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientSessions(request: ListOrganizationRemoteSessionClientSessionsRequest, security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined, options?: QueryHookOptions<OrganizationRemoteSessionClientSessionsQueryData, OrganizationRemoteSessionClientSessionsQueryError>): UseQueryResult<OrganizationRemoteSessionClientSessionsQueryData, OrganizationRemoteSessionClientSessionsQueryError>;
/**
 * listClientSessions organizationRemoteSessions
 *
 * @remarks
 * List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientSessionsSuspense(request: ListOrganizationRemoteSessionClientSessionsRequest, security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined, options?: SuspenseQueryHookOptions<OrganizationRemoteSessionClientSessionsQueryData, OrganizationRemoteSessionClientSessionsQueryError>): UseSuspenseQueryResult<OrganizationRemoteSessionClientSessionsQueryData, OrganizationRemoteSessionClientSessionsQueryError>;
/**
 * listClientSessions organizationRemoteSessions
 *
 * @remarks
 * List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientSessionsInfinite(request: ListOrganizationRemoteSessionClientSessionsRequest, security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined, options?: InfiniteQueryHookOptions<OrganizationRemoteSessionClientSessionsInfiniteQueryData, OrganizationRemoteSessionClientSessionsQueryError>): UseInfiniteQueryResult<InfiniteData<OrganizationRemoteSessionClientSessionsInfiniteQueryData, OrganizationRemoteSessionClientSessionsPageParams>, OrganizationRemoteSessionClientSessionsQueryError>;
/**
 * listClientSessions organizationRemoteSessions
 *
 * @remarks
 * List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientSessionsInfiniteSuspense(request: ListOrganizationRemoteSessionClientSessionsRequest, security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined, options?: SuspenseInfiniteQueryHookOptions<OrganizationRemoteSessionClientSessionsInfiniteQueryData, OrganizationRemoteSessionClientSessionsQueryError>): UseSuspenseInfiniteQueryResult<InfiniteData<OrganizationRemoteSessionClientSessionsInfiniteQueryData, OrganizationRemoteSessionClientSessionsPageParams>, OrganizationRemoteSessionClientSessionsQueryError>;
export declare function setOrganizationRemoteSessionClientSessionsData(client: QueryClient, queryKeyBase: [
    parameters: {
        clientId: string;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
], data: OrganizationRemoteSessionClientSessionsQueryData): OrganizationRemoteSessionClientSessionsQueryData | undefined;
export declare function invalidateOrganizationRemoteSessionClientSessions(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        clientId: string;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllOrganizationRemoteSessionClientSessions(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=organizationRemoteSessionClientSessions.d.ts.map