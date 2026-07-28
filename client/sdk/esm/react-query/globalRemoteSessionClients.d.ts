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
  ListGlobalRemoteSessionClientsRequest,
  ListGlobalRemoteSessionClientsSecurity,
} from "../models/operations/listglobalremotesessionclients.js";
import {
  InfiniteQueryHookOptions,
  QueryHookOptions,
  SuspenseInfiniteQueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGlobalRemoteSessionClientsInfiniteQuery,
  buildGlobalRemoteSessionClientsQuery,
  GlobalRemoteSessionClientsInfiniteQueryData,
  GlobalRemoteSessionClientsPageParams,
  GlobalRemoteSessionClientsQueryData,
  prefetchGlobalRemoteSessionClients,
  prefetchGlobalRemoteSessionClientsInfinite,
  queryKeyGlobalRemoteSessionClients,
  queryKeyGlobalRemoteSessionClientsInfinite,
} from "./globalRemoteSessionClients.core.js";
export {
  buildGlobalRemoteSessionClientsInfiniteQuery,
  buildGlobalRemoteSessionClientsQuery,
  type GlobalRemoteSessionClientsInfiniteQueryData,
  type GlobalRemoteSessionClientsPageParams,
  type GlobalRemoteSessionClientsQueryData,
  prefetchGlobalRemoteSessionClients,
  prefetchGlobalRemoteSessionClientsInfinite,
  queryKeyGlobalRemoteSessionClients,
  queryKeyGlobalRemoteSessionClientsInfinite,
};
export type GlobalRemoteSessionClientsQueryError =
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
 * listGlobalClients adminRemoteSessions
 *
 * @remarks
 * List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.
 */
export declare function useGlobalRemoteSessionClients(
  request: ListGlobalRemoteSessionClientsRequest,
  security?: ListGlobalRemoteSessionClientsSecurity | undefined,
  options?: QueryHookOptions<
    GlobalRemoteSessionClientsQueryData,
    GlobalRemoteSessionClientsQueryError
  >,
): UseQueryResult<
  GlobalRemoteSessionClientsQueryData,
  GlobalRemoteSessionClientsQueryError
>;
/**
 * listGlobalClients adminRemoteSessions
 *
 * @remarks
 * List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.
 */
export declare function useGlobalRemoteSessionClientsSuspense(
  request: ListGlobalRemoteSessionClientsRequest,
  security?: ListGlobalRemoteSessionClientsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GlobalRemoteSessionClientsQueryData,
    GlobalRemoteSessionClientsQueryError
  >,
): UseSuspenseQueryResult<
  GlobalRemoteSessionClientsQueryData,
  GlobalRemoteSessionClientsQueryError
>;
/**
 * listGlobalClients adminRemoteSessions
 *
 * @remarks
 * List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.
 */
export declare function useGlobalRemoteSessionClientsInfinite(
  request: ListGlobalRemoteSessionClientsRequest,
  security?: ListGlobalRemoteSessionClientsSecurity | undefined,
  options?: InfiniteQueryHookOptions<
    GlobalRemoteSessionClientsInfiniteQueryData,
    GlobalRemoteSessionClientsQueryError
  >,
): UseInfiniteQueryResult<
  InfiniteData<
    GlobalRemoteSessionClientsInfiniteQueryData,
    GlobalRemoteSessionClientsPageParams
  >,
  GlobalRemoteSessionClientsQueryError
>;
/**
 * listGlobalClients adminRemoteSessions
 *
 * @remarks
 * List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.
 */
export declare function useGlobalRemoteSessionClientsInfiniteSuspense(
  request: ListGlobalRemoteSessionClientsRequest,
  security?: ListGlobalRemoteSessionClientsSecurity | undefined,
  options?: SuspenseInfiniteQueryHookOptions<
    GlobalRemoteSessionClientsInfiniteQueryData,
    GlobalRemoteSessionClientsQueryError
  >,
): UseSuspenseInfiniteQueryResult<
  InfiniteData<
    GlobalRemoteSessionClientsInfiniteQueryData,
    GlobalRemoteSessionClientsPageParams
  >,
  GlobalRemoteSessionClientsQueryError
>;
export declare function setGlobalRemoteSessionClientsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      remoteSessionIssuerId: string;
      cursor?: string | undefined;
      limit?: number | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: GlobalRemoteSessionClientsQueryData,
): GlobalRemoteSessionClientsQueryData | undefined;
export declare function invalidateGlobalRemoteSessionClients(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        remoteSessionIssuerId: string;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGlobalRemoteSessionClients(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=globalRemoteSessionClients.d.ts.map
