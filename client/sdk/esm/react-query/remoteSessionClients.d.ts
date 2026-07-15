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
  ListRemoteSessionClientsRequest,
  ListRemoteSessionClientsSecurity,
} from "../models/operations/listremotesessionclients.js";
import {
  InfiniteQueryHookOptions,
  QueryHookOptions,
  SuspenseInfiniteQueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRemoteSessionClientsInfiniteQuery,
  buildRemoteSessionClientsQuery,
  prefetchRemoteSessionClients,
  prefetchRemoteSessionClientsInfinite,
  queryKeyRemoteSessionClients,
  queryKeyRemoteSessionClientsInfinite,
  RemoteSessionClientsInfiniteQueryData,
  RemoteSessionClientsPageParams,
  RemoteSessionClientsQueryData,
} from "./remoteSessionClients.core.js";
export {
  buildRemoteSessionClientsInfiniteQuery,
  buildRemoteSessionClientsQuery,
  prefetchRemoteSessionClients,
  prefetchRemoteSessionClientsInfinite,
  queryKeyRemoteSessionClients,
  queryKeyRemoteSessionClientsInfinite,
  type RemoteSessionClientsInfiniteQueryData,
  type RemoteSessionClientsPageParams,
  type RemoteSessionClientsQueryData,
};
export type RemoteSessionClientsQueryError =
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
 * listRemoteSessionClients remoteSessionClients
 *
 * @remarks
 * List remote_session_clients in the caller's project.
 */
export declare function useRemoteSessionClients(
  request?: ListRemoteSessionClientsRequest | undefined,
  security?: ListRemoteSessionClientsSecurity | undefined,
  options?: QueryHookOptions<
    RemoteSessionClientsQueryData,
    RemoteSessionClientsQueryError
  >,
): UseQueryResult<
  RemoteSessionClientsQueryData,
  RemoteSessionClientsQueryError
>;
/**
 * listRemoteSessionClients remoteSessionClients
 *
 * @remarks
 * List remote_session_clients in the caller's project.
 */
export declare function useRemoteSessionClientsSuspense(
  request?: ListRemoteSessionClientsRequest | undefined,
  security?: ListRemoteSessionClientsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RemoteSessionClientsQueryData,
    RemoteSessionClientsQueryError
  >,
): UseSuspenseQueryResult<
  RemoteSessionClientsQueryData,
  RemoteSessionClientsQueryError
>;
/**
 * listRemoteSessionClients remoteSessionClients
 *
 * @remarks
 * List remote_session_clients in the caller's project.
 */
export declare function useRemoteSessionClientsInfinite(
  request?: ListRemoteSessionClientsRequest | undefined,
  security?: ListRemoteSessionClientsSecurity | undefined,
  options?: InfiniteQueryHookOptions<
    RemoteSessionClientsInfiniteQueryData,
    RemoteSessionClientsQueryError
  >,
): UseInfiniteQueryResult<
  InfiniteData<
    RemoteSessionClientsInfiniteQueryData,
    RemoteSessionClientsPageParams
  >,
  RemoteSessionClientsQueryError
>;
/**
 * listRemoteSessionClients remoteSessionClients
 *
 * @remarks
 * List remote_session_clients in the caller's project.
 */
export declare function useRemoteSessionClientsInfiniteSuspense(
  request?: ListRemoteSessionClientsRequest | undefined,
  security?: ListRemoteSessionClientsSecurity | undefined,
  options?: SuspenseInfiniteQueryHookOptions<
    RemoteSessionClientsInfiniteQueryData,
    RemoteSessionClientsQueryError
  >,
): UseSuspenseInfiniteQueryResult<
  InfiniteData<
    RemoteSessionClientsInfiniteQueryData,
    RemoteSessionClientsPageParams
  >,
  RemoteSessionClientsQueryError
>;
export declare function setRemoteSessionClientsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      remoteSessionIssuerId?: string | undefined;
      userSessionIssuerId?: string | undefined;
      cursor?: string | undefined;
      limit?: number | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RemoteSessionClientsQueryData,
): RemoteSessionClientsQueryData | undefined;
export declare function invalidateRemoteSessionClients(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        remoteSessionIssuerId?: string | undefined;
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
export declare function invalidateAllRemoteSessionClients(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=remoteSessionClients.d.ts.map
