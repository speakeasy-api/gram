import { InfiniteData, InvalidateQueryFilters, QueryClient, UseInfiniteQueryResult, UseQueryResult, UseSuspenseInfiniteQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListUserSessionIssuersRequest, ListUserSessionIssuersSecurity } from "../models/operations/listusersessionissuers.js";
import { InfiniteQueryHookOptions, QueryHookOptions, SuspenseInfiniteQueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildUserSessionIssuersInfiniteQuery, buildUserSessionIssuersQuery, prefetchUserSessionIssuers, prefetchUserSessionIssuersInfinite, queryKeyUserSessionIssuers, queryKeyUserSessionIssuersInfinite, UserSessionIssuersInfiniteQueryData, UserSessionIssuersPageParams, UserSessionIssuersQueryData } from "./userSessionIssuers.core.js";
export { buildUserSessionIssuersInfiniteQuery, buildUserSessionIssuersQuery, prefetchUserSessionIssuers, prefetchUserSessionIssuersInfinite, queryKeyUserSessionIssuers, queryKeyUserSessionIssuersInfinite, type UserSessionIssuersInfiniteQueryData, type UserSessionIssuersPageParams, type UserSessionIssuersQueryData, };
export type UserSessionIssuersQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listUserSessionIssuers userSessionIssuers
 *
 * @remarks
 * List user_session_issuers in the caller's project.
 */
export declare function useUserSessionIssuers(request?: ListUserSessionIssuersRequest | undefined, security?: ListUserSessionIssuersSecurity | undefined, options?: QueryHookOptions<UserSessionIssuersQueryData, UserSessionIssuersQueryError>): UseQueryResult<UserSessionIssuersQueryData, UserSessionIssuersQueryError>;
/**
 * listUserSessionIssuers userSessionIssuers
 *
 * @remarks
 * List user_session_issuers in the caller's project.
 */
export declare function useUserSessionIssuersSuspense(request?: ListUserSessionIssuersRequest | undefined, security?: ListUserSessionIssuersSecurity | undefined, options?: SuspenseQueryHookOptions<UserSessionIssuersQueryData, UserSessionIssuersQueryError>): UseSuspenseQueryResult<UserSessionIssuersQueryData, UserSessionIssuersQueryError>;
/**
 * listUserSessionIssuers userSessionIssuers
 *
 * @remarks
 * List user_session_issuers in the caller's project.
 */
export declare function useUserSessionIssuersInfinite(request?: ListUserSessionIssuersRequest | undefined, security?: ListUserSessionIssuersSecurity | undefined, options?: InfiniteQueryHookOptions<UserSessionIssuersInfiniteQueryData, UserSessionIssuersQueryError>): UseInfiniteQueryResult<InfiniteData<UserSessionIssuersInfiniteQueryData, UserSessionIssuersPageParams>, UserSessionIssuersQueryError>;
/**
 * listUserSessionIssuers userSessionIssuers
 *
 * @remarks
 * List user_session_issuers in the caller's project.
 */
export declare function useUserSessionIssuersInfiniteSuspense(request?: ListUserSessionIssuersRequest | undefined, security?: ListUserSessionIssuersSecurity | undefined, options?: SuspenseInfiniteQueryHookOptions<UserSessionIssuersInfiniteQueryData, UserSessionIssuersQueryError>): UseSuspenseInfiniteQueryResult<InfiniteData<UserSessionIssuersInfiniteQueryData, UserSessionIssuersPageParams>, UserSessionIssuersQueryError>;
export declare function setUserSessionIssuersData(client: QueryClient, queryKeyBase: [
    parameters: {
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: UserSessionIssuersQueryData): UserSessionIssuersQueryData | undefined;
export declare function invalidateUserSessionIssuers(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllUserSessionIssuers(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=userSessionIssuers.d.ts.map