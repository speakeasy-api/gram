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
  ListToolsRequest,
  ListToolsSecurity,
  ToolTypes,
} from "../models/operations/listtools.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListToolsQuery,
  ListToolsQueryData,
  prefetchListTools,
  queryKeyListTools,
} from "./listTools.core.js";
export {
  buildListToolsQuery,
  type ListToolsQueryData,
  prefetchListTools,
  queryKeyListTools,
};
export type ListToolsQueryError =
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
 * listTools tools
 *
 * @remarks
 * List all tools for a project
 */
export declare function useListTools(
  request?: ListToolsRequest | undefined,
  security?: ListToolsSecurity | undefined,
  options?: QueryHookOptions<ListToolsQueryData, ListToolsQueryError>,
): UseQueryResult<ListToolsQueryData, ListToolsQueryError>;
/**
 * listTools tools
 *
 * @remarks
 * List all tools for a project
 */
export declare function useListToolsSuspense(
  request?: ListToolsRequest | undefined,
  security?: ListToolsSecurity | undefined,
  options?: SuspenseQueryHookOptions<ListToolsQueryData, ListToolsQueryError>,
): UseSuspenseQueryResult<ListToolsQueryData, ListToolsQueryError>;
export declare function setListToolsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      cursor?: string | undefined;
      limit?: number | undefined;
      deploymentId?: string | undefined;
      urnPrefix?: string | undefined;
      toolTypes?: Array<ToolTypes> | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListToolsQueryData,
): ListToolsQueryData | undefined;
export declare function invalidateListTools(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        cursor?: string | undefined;
        limit?: number | undefined;
        deploymentId?: string | undefined;
        urnPrefix?: string | undefined;
        toolTypes?: Array<ToolTypes> | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListTools(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listTools.d.ts.map
