import { InfiniteData, InvalidateQueryFilters, QueryClient, UseInfiniteQueryResult, UseQueryResult, UseSuspenseInfiniteQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListUserSessionClientsRequest, ListUserSessionClientsSecurity } from "../models/operations/listusersessionclients.js";
import { InfiniteQueryHookOptions, QueryHookOptions, SuspenseInfiniteQueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildUserSessionClientsInfiniteQuery, buildUserSessionClientsQuery, prefetchUserSessionClients, prefetchUserSessionClientsInfinite, queryKeyUserSessionClients, queryKeyUserSessionClientsInfinite, UserSessionClientsInfiniteQueryData, UserSessionClientsPageParams, UserSessionClientsQueryData } from "./userSessionClients.core.js";
export { buildUserSessionClientsInfiniteQuery, buildUserSessionClientsQuery, prefetchUserSessionClients, prefetchUserSessionClientsInfinite, queryKeyUserSessionClients, queryKeyUserSessionClientsInfinite, type UserSessionClientsInfiniteQueryData, type UserSessionClientsPageParams, type UserSessionClientsQueryData, };
export type UserSessionClientsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listUserSessionClients userSessionClients
 *
 * @remarks
 * List user_session_clients in the caller's project.
 */
export declare function useUserSessionClients(request?: ListUserSessionClientsRequest | undefined, security?: ListUserSessionClientsSecurity | undefined, options?: QueryHookOptions<UserSessionClientsQueryData, UserSessionClientsQueryError>): UseQueryResult<UserSessionClientsQueryData, UserSessionClientsQueryError>;
/**
 * listUserSessionClients userSessionClients
 *
 * @remarks
 * List user_session_clients in the caller's project.
 */
export declare function useUserSessionClientsSuspense(request?: ListUserSessionClientsRequest | undefined, security?: ListUserSessionClientsSecurity | undefined, options?: SuspenseQueryHookOptions<UserSessionClientsQueryData, UserSessionClientsQueryError>): UseSuspenseQueryResult<UserSessionClientsQueryData, UserSessionClientsQueryError>;
/**
 * listUserSessionClients userSessionClients
 *
 * @remarks
 * List user_session_clients in the caller's project.
 */
export declare function useUserSessionClientsInfinite(request?: ListUserSessionClientsRequest | undefined, security?: ListUserSessionClientsSecurity | undefined, options?: InfiniteQueryHookOptions<UserSessionClientsInfiniteQueryData, UserSessionClientsQueryError>): UseInfiniteQueryResult<InfiniteData<UserSessionClientsInfiniteQueryData, UserSessionClientsPageParams>, UserSessionClientsQueryError>;
/**
 * listUserSessionClients userSessionClients
 *
 * @remarks
 * List user_session_clients in the caller's project.
 */
export declare function useUserSessionClientsInfiniteSuspense(request?: ListUserSessionClientsRequest | undefined, security?: ListUserSessionClientsSecurity | undefined, options?: SuspenseInfiniteQueryHookOptions<UserSessionClientsInfiniteQueryData, UserSessionClientsQueryError>): UseSuspenseInfiniteQueryResult<InfiniteData<UserSessionClientsInfiniteQueryData, UserSessionClientsPageParams>, UserSessionClientsQueryError>;
export declare function setUserSessionClientsData(client: QueryClient, queryKeyBase: [
    parameters: {
        userSessionIssuerId?: string | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: UserSessionClientsQueryData): UserSessionClientsQueryData | undefined;
export declare function invalidateUserSessionClients(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        userSessionIssuerId?: string | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllUserSessionClients(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=userSessionClients.d.ts.map