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
  ListUserSessionConsentsRequest,
  ListUserSessionConsentsSecurity,
} from "../models/operations/listusersessionconsents.js";
import {
  InfiniteQueryHookOptions,
  QueryHookOptions,
  SuspenseInfiniteQueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildUserSessionConsentsInfiniteQuery,
  buildUserSessionConsentsQuery,
  prefetchUserSessionConsents,
  prefetchUserSessionConsentsInfinite,
  queryKeyUserSessionConsents,
  queryKeyUserSessionConsentsInfinite,
  UserSessionConsentsInfiniteQueryData,
  UserSessionConsentsPageParams,
  UserSessionConsentsQueryData,
} from "./userSessionConsents.core.js";
export {
  buildUserSessionConsentsInfiniteQuery,
  buildUserSessionConsentsQuery,
  prefetchUserSessionConsents,
  prefetchUserSessionConsentsInfinite,
  queryKeyUserSessionConsents,
  queryKeyUserSessionConsentsInfinite,
  type UserSessionConsentsInfiniteQueryData,
  type UserSessionConsentsPageParams,
  type UserSessionConsentsQueryData,
};
export type UserSessionConsentsQueryError =
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
 * listUserSessionConsents userSessionConsents
 *
 * @remarks
 * List consent records for the caller's project.
 */
export declare function useUserSessionConsents(
  request?: ListUserSessionConsentsRequest | undefined,
  security?: ListUserSessionConsentsSecurity | undefined,
  options?: QueryHookOptions<
    UserSessionConsentsQueryData,
    UserSessionConsentsQueryError
  >,
): UseQueryResult<UserSessionConsentsQueryData, UserSessionConsentsQueryError>;
/**
 * listUserSessionConsents userSessionConsents
 *
 * @remarks
 * List consent records for the caller's project.
 */
export declare function useUserSessionConsentsSuspense(
  request?: ListUserSessionConsentsRequest | undefined,
  security?: ListUserSessionConsentsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    UserSessionConsentsQueryData,
    UserSessionConsentsQueryError
  >,
): UseSuspenseQueryResult<
  UserSessionConsentsQueryData,
  UserSessionConsentsQueryError
>;
/**
 * listUserSessionConsents userSessionConsents
 *
 * @remarks
 * List consent records for the caller's project.
 */
export declare function useUserSessionConsentsInfinite(
  request?: ListUserSessionConsentsRequest | undefined,
  security?: ListUserSessionConsentsSecurity | undefined,
  options?: InfiniteQueryHookOptions<
    UserSessionConsentsInfiniteQueryData,
    UserSessionConsentsQueryError
  >,
): UseInfiniteQueryResult<
  InfiniteData<
    UserSessionConsentsInfiniteQueryData,
    UserSessionConsentsPageParams
  >,
  UserSessionConsentsQueryError
>;
/**
 * listUserSessionConsents userSessionConsents
 *
 * @remarks
 * List consent records for the caller's project.
 */
export declare function useUserSessionConsentsInfiniteSuspense(
  request?: ListUserSessionConsentsRequest | undefined,
  security?: ListUserSessionConsentsSecurity | undefined,
  options?: SuspenseInfiniteQueryHookOptions<
    UserSessionConsentsInfiniteQueryData,
    UserSessionConsentsQueryError
  >,
): UseSuspenseInfiniteQueryResult<
  InfiniteData<
    UserSessionConsentsInfiniteQueryData,
    UserSessionConsentsPageParams
  >,
  UserSessionConsentsQueryError
>;
export declare function setUserSessionConsentsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      subjectUrn?: string | undefined;
      userSessionClientId?: string | undefined;
      userSessionIssuerId?: string | undefined;
      cursor?: string | undefined;
      limit?: number | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: UserSessionConsentsQueryData,
): UserSessionConsentsQueryData | undefined;
export declare function invalidateUserSessionConsents(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        subjectUrn?: string | undefined;
        userSessionClientId?: string | undefined;
        userSessionIssuerId?: string | undefined;
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
export declare function invalidateAllUserSessionConsents(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=userSessionConsents.d.ts.map
