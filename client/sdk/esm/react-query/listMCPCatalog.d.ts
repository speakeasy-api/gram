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
  ListMCPCatalogRequest,
  ListMCPCatalogSecurity,
} from "../models/operations/listmcpcatalog.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListMCPCatalogQuery,
  ListMCPCatalogQueryData,
  prefetchListMCPCatalog,
  queryKeyListMCPCatalog,
} from "./listMCPCatalog.core.js";
export {
  buildListMCPCatalogQuery,
  type ListMCPCatalogQueryData,
  prefetchListMCPCatalog,
  queryKeyListMCPCatalog,
};
export type ListMCPCatalogQueryError =
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
 * listCatalog mcpRegistries
 *
 * @remarks
 * List available MCP servers from configured registries
 */
export declare function useListMCPCatalog(
  request?: ListMCPCatalogRequest | undefined,
  security?: ListMCPCatalogSecurity | undefined,
  options?: QueryHookOptions<ListMCPCatalogQueryData, ListMCPCatalogQueryError>,
): UseQueryResult<ListMCPCatalogQueryData, ListMCPCatalogQueryError>;
/**
 * listCatalog mcpRegistries
 *
 * @remarks
 * List available MCP servers from configured registries
 */
export declare function useListMCPCatalogSuspense(
  request?: ListMCPCatalogRequest | undefined,
  security?: ListMCPCatalogSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListMCPCatalogQueryData,
    ListMCPCatalogQueryError
  >,
): UseSuspenseQueryResult<ListMCPCatalogQueryData, ListMCPCatalogQueryError>;
export declare function setListMCPCatalogData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      registryId?: string | undefined;
      search?: string | undefined;
      cursor?: string | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListMCPCatalogQueryData,
): ListMCPCatalogQueryData | undefined;
export declare function invalidateListMCPCatalog(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        registryId?: string | undefined;
        search?: string | undefined;
        cursor?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListMCPCatalog(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listMCPCatalog.d.ts.map
