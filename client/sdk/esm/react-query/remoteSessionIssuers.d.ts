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
  ListRemoteSessionIssuersRequest,
  ListRemoteSessionIssuersSecurity,
} from "../models/operations/listremotesessionissuers.js";
import {
  InfiniteQueryHookOptions,
  QueryHookOptions,
  SuspenseInfiniteQueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRemoteSessionIssuersInfiniteQuery,
  buildRemoteSessionIssuersQuery,
  prefetchRemoteSessionIssuers,
  prefetchRemoteSessionIssuersInfinite,
  queryKeyRemoteSessionIssuers,
  queryKeyRemoteSessionIssuersInfinite,
  RemoteSessionIssuersInfiniteQueryData,
  RemoteSessionIssuersPageParams,
  RemoteSessionIssuersQueryData,
} from "./remoteSessionIssuers.core.js";
export {
  buildRemoteSessionIssuersInfiniteQuery,
  buildRemoteSessionIssuersQuery,
  prefetchRemoteSessionIssuers,
  prefetchRemoteSessionIssuersInfinite,
  queryKeyRemoteSessionIssuers,
  queryKeyRemoteSessionIssuersInfinite,
  type RemoteSessionIssuersInfiniteQueryData,
  type RemoteSessionIssuersPageParams,
  type RemoteSessionIssuersQueryData,
};
export type RemoteSessionIssuersQueryError =
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
 * listRemoteSessionIssuers remoteSessionIssuers
 *
 * @remarks
 * List remote_session_issuers in the caller's project.
 */
export declare function useRemoteSessionIssuers(
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: QueryHookOptions<
    RemoteSessionIssuersQueryData,
    RemoteSessionIssuersQueryError
  >,
): UseQueryResult<
  RemoteSessionIssuersQueryData,
  RemoteSessionIssuersQueryError
>;
/**
 * listRemoteSessionIssuers remoteSessionIssuers
 *
 * @remarks
 * List remote_session_issuers in the caller's project.
 */
export declare function useRemoteSessionIssuersSuspense(
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RemoteSessionIssuersQueryData,
    RemoteSessionIssuersQueryError
  >,
): UseSuspenseQueryResult<
  RemoteSessionIssuersQueryData,
  RemoteSessionIssuersQueryError
>;
/**
 * listRemoteSessionIssuers remoteSessionIssuers
 *
 * @remarks
 * List remote_session_issuers in the caller's project.
 */
export declare function useRemoteSessionIssuersInfinite(
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: InfiniteQueryHookOptions<
    RemoteSessionIssuersInfiniteQueryData,
    RemoteSessionIssuersQueryError
  >,
): UseInfiniteQueryResult<
  InfiniteData<
    RemoteSessionIssuersInfiniteQueryData,
    RemoteSessionIssuersPageParams
  >,
  RemoteSessionIssuersQueryError
>;
/**
 * listRemoteSessionIssuers remoteSessionIssuers
 *
 * @remarks
 * List remote_session_issuers in the caller's project.
 */
export declare function useRemoteSessionIssuersInfiniteSuspense(
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: SuspenseInfiniteQueryHookOptions<
    RemoteSessionIssuersInfiniteQueryData,
    RemoteSessionIssuersQueryError
  >,
): UseSuspenseInfiniteQueryResult<
  InfiniteData<
    RemoteSessionIssuersInfiniteQueryData,
    RemoteSessionIssuersPageParams
  >,
  RemoteSessionIssuersQueryError
>;
export declare function setRemoteSessionIssuersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      cursor?: string | undefined;
      limit?: number | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RemoteSessionIssuersQueryData,
): RemoteSessionIssuersQueryData | undefined;
export declare function invalidateRemoteSessionIssuers(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
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
export declare function invalidateAllRemoteSessionIssuers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=remoteSessionIssuers.d.ts.map
