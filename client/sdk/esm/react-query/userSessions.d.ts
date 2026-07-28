import {
  InfiniteData,
  InvalidateQueryFilters,
  QueryClient,
  UseInfiniteQueryResult,
  UseQueryResult,
  UseSuspenseInfiniteQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  ListUserSessionsQueryParamStatus,
  ListUserSessionsRequest,
  ListUserSessionsSecurity,
} from "../models/operations/listusersessions.js";
import {
  InfiniteQueryHookOptions,
  QueryHookOptions,
  SuspenseInfiniteQueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildUserSessionsInfiniteQuery,
  buildUserSessionsQuery,
  prefetchUserSessions,
  prefetchUserSessionsInfinite,
  queryKeyUserSessions,
  queryKeyUserSessionsInfinite,
  UserSessionsInfiniteQueryData,
  UserSessionsPageParams,
  UserSessionsQueryData,
} from "./userSessions.core.js";
export {
  buildUserSessionsInfiniteQuery,
  buildUserSessionsQuery,
  prefetchUserSessions,
  prefetchUserSessionsInfinite,
  queryKeyUserSessions,
  queryKeyUserSessionsInfinite,
  type UserSessionsInfiniteQueryData,
  type UserSessionsPageParams,
  type UserSessionsQueryData,
};
export type UserSessionsQueryError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * listUserSessions userSessions
 *
 * @remarks
 * List issued user_sessions in the caller's project. refresh_token_hash is never returned.
 */
export declare function useUserSessions(
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: QueryHookOptions<UserSessionsQueryData, UserSessionsQueryError>,
): UseQueryResult<UserSessionsQueryData, UserSessionsQueryError>;
/**
 * listUserSessions userSessions
 *
 * @remarks
 * List issued user_sessions in the caller's project. refresh_token_hash is never returned.
 */
export declare function useUserSessionsSuspense(
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    UserSessionsQueryData,
    UserSessionsQueryError
  >,
): UseSuspenseQueryResult<UserSessionsQueryData, UserSessionsQueryError>;
/**
 * listUserSessions userSessions
 *
 * @remarks
 * List issued user_sessions in the caller's project. refresh_token_hash is never returned.
 */
export declare function useUserSessionsInfinite(
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: InfiniteQueryHookOptions<
    UserSessionsInfiniteQueryData,
    UserSessionsQueryError
  >,
): UseInfiniteQueryResult<
  InfiniteData<UserSessionsInfiniteQueryData, UserSessionsPageParams>,
  UserSessionsQueryError
>;
/**
 * listUserSessions userSessions
 *
 * @remarks
 * List issued user_sessions in the caller's project. refresh_token_hash is never returned.
 */
export declare function useUserSessionsInfiniteSuspense(
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: SuspenseInfiniteQueryHookOptions<
    UserSessionsInfiniteQueryData,
    UserSessionsQueryError
  >,
): UseSuspenseInfiniteQueryResult<
  InfiniteData<UserSessionsInfiniteQueryData, UserSessionsPageParams>,
  UserSessionsQueryError
>;
export declare function setUserSessionsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      subjectUrn?: string | undefined;
      userSessionIssuerId?: string | undefined;
      status?: ListUserSessionsQueryParamStatus | undefined;
      clientId?: string | undefined;
      cursor?: string | undefined;
      limit?: number | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: UserSessionsQueryData,
): UserSessionsQueryData | undefined;
export declare function invalidateUserSessions(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        subjectUrn?: string | undefined;
        userSessionIssuerId?: string | undefined;
        status?: ListUserSessionsQueryParamStatus | undefined;
        clientId?: string | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllUserSessions(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=userSessions.d.ts.map
