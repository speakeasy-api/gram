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
  ListResourcesRequest,
  ListResourcesSecurity,
} from "../models/operations/listresources.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListResourcesQuery,
  ListResourcesQueryData,
  prefetchListResources,
  queryKeyListResources,
} from "./listResources.core.js";
export {
  buildListResourcesQuery,
  type ListResourcesQueryData,
  prefetchListResources,
  queryKeyListResources,
};
export type ListResourcesQueryError =
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
 * listResources resources
 *
 * @remarks
 * List all resources for a project
 */
export declare function useListResources(
  request?: ListResourcesRequest | undefined,
  security?: ListResourcesSecurity | undefined,
  options?: QueryHookOptions<ListResourcesQueryData, ListResourcesQueryError>,
): UseQueryResult<ListResourcesQueryData, ListResourcesQueryError>;
/**
 * listResources resources
 *
 * @remarks
 * List all resources for a project
 */
export declare function useListResourcesSuspense(
  request?: ListResourcesRequest | undefined,
  security?: ListResourcesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListResourcesQueryData,
    ListResourcesQueryError
  >,
): UseSuspenseQueryResult<ListResourcesQueryData, ListResourcesQueryError>;
export declare function setListResourcesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      cursor?: string | undefined;
      limit?: number | undefined;
      deploymentId?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListResourcesQueryData,
): ListResourcesQueryData | undefined;
export declare function invalidateListResources(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        cursor?: string | undefined;
        limit?: number | undefined;
        deploymentId?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListResources(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listResources.d.ts.map
