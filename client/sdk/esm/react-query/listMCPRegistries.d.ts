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
  ListMCPRegistriesRequest,
  ListMCPRegistriesSecurity,
} from "../models/operations/listmcpregistries.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListMCPRegistriesQuery,
  ListMCPRegistriesQueryData,
  prefetchListMCPRegistries,
  queryKeyListMCPRegistries,
} from "./listMCPRegistries.core.js";
export {
  buildListMCPRegistriesQuery,
  type ListMCPRegistriesQueryData,
  prefetchListMCPRegistries,
  queryKeyListMCPRegistries,
};
export type ListMCPRegistriesQueryError =
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
 * listRegistries mcpRegistries
 *
 * @remarks
 * List all MCP registries (admin only)
 */
export declare function useListMCPRegistries(
  request?: ListMCPRegistriesRequest | undefined,
  security?: ListMCPRegistriesSecurity | undefined,
  options?: QueryHookOptions<
    ListMCPRegistriesQueryData,
    ListMCPRegistriesQueryError
  >,
): UseQueryResult<ListMCPRegistriesQueryData, ListMCPRegistriesQueryError>;
/**
 * listRegistries mcpRegistries
 *
 * @remarks
 * List all MCP registries (admin only)
 */
export declare function useListMCPRegistriesSuspense(
  request?: ListMCPRegistriesRequest | undefined,
  security?: ListMCPRegistriesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListMCPRegistriesQueryData,
    ListMCPRegistriesQueryError
  >,
): UseSuspenseQueryResult<
  ListMCPRegistriesQueryData,
  ListMCPRegistriesQueryError
>;
export declare function setListMCPRegistriesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListMCPRegistriesQueryData,
): ListMCPRegistriesQueryData | undefined;
export declare function invalidateListMCPRegistries(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListMCPRegistries(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listMCPRegistries.d.ts.map
