import { InfiniteData, InvalidateQueryFilters, QueryClient, UseInfiniteQueryResult, UseQueryResult, UseSuspenseInfiniteQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListOrganizationRemoteSessionClientsRequest, ListOrganizationRemoteSessionClientsSecurity } from "../models/operations/listorganizationremotesessionclients.js";
import { InfiniteQueryHookOptions, QueryHookOptions, SuspenseInfiniteQueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildOrganizationRemoteSessionClientsInfiniteQuery, buildOrganizationRemoteSessionClientsQuery, OrganizationRemoteSessionClientsInfiniteQueryData, OrganizationRemoteSessionClientsPageParams, OrganizationRemoteSessionClientsQueryData, prefetchOrganizationRemoteSessionClients, prefetchOrganizationRemoteSessionClientsInfinite, queryKeyOrganizationRemoteSessionClients, queryKeyOrganizationRemoteSessionClientsInfinite } from "./organizationRemoteSessionClients.core.js";
export { buildOrganizationRemoteSessionClientsInfiniteQuery, buildOrganizationRemoteSessionClientsQuery, type OrganizationRemoteSessionClientsInfiniteQueryData, type OrganizationRemoteSessionClientsPageParams, type OrganizationRemoteSessionClientsQueryData, prefetchOrganizationRemoteSessionClients, prefetchOrganizationRemoteSessionClientsInfinite, queryKeyOrganizationRemoteSessionClients, queryKeyOrganizationRemoteSessionClientsInfinite, };
export type OrganizationRemoteSessionClientsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listClients organizationRemoteSessionClients
 *
 * @remarks
 * List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClients(request: ListOrganizationRemoteSessionClientsRequest, security?: ListOrganizationRemoteSessionClientsSecurity | undefined, options?: QueryHookOptions<OrganizationRemoteSessionClientsQueryData, OrganizationRemoteSessionClientsQueryError>): UseQueryResult<OrganizationRemoteSessionClientsQueryData, OrganizationRemoteSessionClientsQueryError>;
/**
 * listClients organizationRemoteSessionClients
 *
 * @remarks
 * List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientsSuspense(request: ListOrganizationRemoteSessionClientsRequest, security?: ListOrganizationRemoteSessionClientsSecurity | undefined, options?: SuspenseQueryHookOptions<OrganizationRemoteSessionClientsQueryData, OrganizationRemoteSessionClientsQueryError>): UseSuspenseQueryResult<OrganizationRemoteSessionClientsQueryData, OrganizationRemoteSessionClientsQueryError>;
/**
 * listClients organizationRemoteSessionClients
 *
 * @remarks
 * List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientsInfinite(request: ListOrganizationRemoteSessionClientsRequest, security?: ListOrganizationRemoteSessionClientsSecurity | undefined, options?: InfiniteQueryHookOptions<OrganizationRemoteSessionClientsInfiniteQueryData, OrganizationRemoteSessionClientsQueryError>): UseInfiniteQueryResult<InfiniteData<OrganizationRemoteSessionClientsInfiniteQueryData, OrganizationRemoteSessionClientsPageParams>, OrganizationRemoteSessionClientsQueryError>;
/**
 * listClients organizationRemoteSessionClients
 *
 * @remarks
 * List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientsInfiniteSuspense(request: ListOrganizationRemoteSessionClientsRequest, security?: ListOrganizationRemoteSessionClientsSecurity | undefined, options?: SuspenseInfiniteQueryHookOptions<OrganizationRemoteSessionClientsInfiniteQueryData, OrganizationRemoteSessionClientsQueryError>): UseSuspenseInfiniteQueryResult<InfiniteData<OrganizationRemoteSessionClientsInfiniteQueryData, OrganizationRemoteSessionClientsPageParams>, OrganizationRemoteSessionClientsQueryError>;
export declare function setOrganizationRemoteSessionClientsData(client: QueryClient, queryKeyBase: [
    parameters: {
        issuerId: string;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
], data: OrganizationRemoteSessionClientsQueryData): OrganizationRemoteSessionClientsQueryData | undefined;
export declare function invalidateOrganizationRemoteSessionClients(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        issuerId: string;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllOrganizationRemoteSessionClients(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=organizationRemoteSessionClients.d.ts.map