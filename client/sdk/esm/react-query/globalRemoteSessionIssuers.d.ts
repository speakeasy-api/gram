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
  ListGlobalRemoteSessionIssuersRequest,
  ListGlobalRemoteSessionIssuersSecurity,
} from "../models/operations/listglobalremotesessionissuers.js";
import {
  InfiniteQueryHookOptions,
  QueryHookOptions,
  SuspenseInfiniteQueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGlobalRemoteSessionIssuersInfiniteQuery,
  buildGlobalRemoteSessionIssuersQuery,
  GlobalRemoteSessionIssuersInfiniteQueryData,
  GlobalRemoteSessionIssuersPageParams,
  GlobalRemoteSessionIssuersQueryData,
  prefetchGlobalRemoteSessionIssuers,
  prefetchGlobalRemoteSessionIssuersInfinite,
  queryKeyGlobalRemoteSessionIssuers,
  queryKeyGlobalRemoteSessionIssuersInfinite,
} from "./globalRemoteSessionIssuers.core.js";
export {
  buildGlobalRemoteSessionIssuersInfiniteQuery,
  buildGlobalRemoteSessionIssuersQuery,
  type GlobalRemoteSessionIssuersInfiniteQueryData,
  type GlobalRemoteSessionIssuersPageParams,
  type GlobalRemoteSessionIssuersQueryData,
  prefetchGlobalRemoteSessionIssuers,
  prefetchGlobalRemoteSessionIssuersInfinite,
  queryKeyGlobalRemoteSessionIssuers,
  queryKeyGlobalRemoteSessionIssuersInfinite,
};
export type GlobalRemoteSessionIssuersQueryError =
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
 * listGlobalIssuers adminRemoteSessions
 *
 * @remarks
 * List global remote_session_issuers. Requires platform admin.
 */
export declare function useGlobalRemoteSessionIssuers(
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: QueryHookOptions<
    GlobalRemoteSessionIssuersQueryData,
    GlobalRemoteSessionIssuersQueryError
  >,
): UseQueryResult<
  GlobalRemoteSessionIssuersQueryData,
  GlobalRemoteSessionIssuersQueryError
>;
/**
 * listGlobalIssuers adminRemoteSessions
 *
 * @remarks
 * List global remote_session_issuers. Requires platform admin.
 */
export declare function useGlobalRemoteSessionIssuersSuspense(
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GlobalRemoteSessionIssuersQueryData,
    GlobalRemoteSessionIssuersQueryError
  >,
): UseSuspenseQueryResult<
  GlobalRemoteSessionIssuersQueryData,
  GlobalRemoteSessionIssuersQueryError
>;
/**
 * listGlobalIssuers adminRemoteSessions
 *
 * @remarks
 * List global remote_session_issuers. Requires platform admin.
 */
export declare function useGlobalRemoteSessionIssuersInfinite(
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: InfiniteQueryHookOptions<
    GlobalRemoteSessionIssuersInfiniteQueryData,
    GlobalRemoteSessionIssuersQueryError
  >,
): UseInfiniteQueryResult<
  InfiniteData<
    GlobalRemoteSessionIssuersInfiniteQueryData,
    GlobalRemoteSessionIssuersPageParams
  >,
  GlobalRemoteSessionIssuersQueryError
>;
/**
 * listGlobalIssuers adminRemoteSessions
 *
 * @remarks
 * List global remote_session_issuers. Requires platform admin.
 */
export declare function useGlobalRemoteSessionIssuersInfiniteSuspense(
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: SuspenseInfiniteQueryHookOptions<
    GlobalRemoteSessionIssuersInfiniteQueryData,
    GlobalRemoteSessionIssuersQueryError
  >,
): UseSuspenseInfiniteQueryResult<
  InfiniteData<
    GlobalRemoteSessionIssuersInfiniteQueryData,
    GlobalRemoteSessionIssuersPageParams
  >,
  GlobalRemoteSessionIssuersQueryError
>;
export declare function setGlobalRemoteSessionIssuersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      cursor?: string | undefined;
      limit?: number | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: GlobalRemoteSessionIssuersQueryData,
): GlobalRemoteSessionIssuersQueryData | undefined;
export declare function invalidateGlobalRemoteSessionIssuers(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGlobalRemoteSessionIssuers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=globalRemoteSessionIssuers.d.ts.map
