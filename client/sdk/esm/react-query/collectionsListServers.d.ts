import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
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
  ListCollectionServersRequest,
  ListCollectionServersSecurity,
} from "../models/operations/listcollectionservers.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildCollectionsListServersQuery,
  CollectionsListServersQueryData,
  prefetchCollectionsListServers,
  queryKeyCollectionsListServers,
} from "./collectionsListServers.core.js";
export {
  buildCollectionsListServersQuery,
  type CollectionsListServersQueryData,
  prefetchCollectionsListServers,
  queryKeyCollectionsListServers,
};
export type CollectionsListServersQueryError =
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
 * listServers collections
 *
 * @remarks
 * List published MCP servers from a collection
 */
export declare function useCollectionsListServers(
  request: ListCollectionServersRequest,
  security?: ListCollectionServersSecurity | undefined,
  options?: QueryHookOptions<
    CollectionsListServersQueryData,
    CollectionsListServersQueryError
  >,
): UseQueryResult<
  CollectionsListServersQueryData,
  CollectionsListServersQueryError
>;
/**
 * listServers collections
 *
 * @remarks
 * List published MCP servers from a collection
 */
export declare function useCollectionsListServersSuspense(
  request: ListCollectionServersRequest,
  security?: ListCollectionServersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    CollectionsListServersQueryData,
    CollectionsListServersQueryError
  >,
): UseSuspenseQueryResult<
  CollectionsListServersQueryData,
  CollectionsListServersQueryError
>;
export declare function setCollectionsListServersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      collectionSlug: string;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
    },
  ],
  data: CollectionsListServersQueryData,
): CollectionsListServersQueryData | undefined;
export declare function invalidateCollectionsListServers(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        collectionSlug: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllCollectionsListServers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=collectionsListServers.d.ts.map
