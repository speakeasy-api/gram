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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildServeMCPRegistryQuery,
  prefetchServeMCPRegistry,
  queryKeyServeMCPRegistry,
  ServeMCPRegistryQueryData,
} from "./serveMCPRegistry.core.js";
export {
  buildServeMCPRegistryQuery,
  prefetchServeMCPRegistry,
  queryKeyServeMCPRegistry,
  type ServeMCPRegistryQueryData,
};
export type ServeMCPRegistryQueryError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * serve mcpRegistries
 *
 * @remarks
 * Serve MCP servers from a specific registry by slug
 */
export declare function useServeMCPRegistry(
  request: operations.ServeMCPRegistryRequest,
  security?: operations.ServeMCPRegistrySecurity | undefined,
  options?: QueryHookOptions<
    ServeMCPRegistryQueryData,
    ServeMCPRegistryQueryError
  >,
): UseQueryResult<ServeMCPRegistryQueryData, ServeMCPRegistryQueryError>;
/**
 * serve mcpRegistries
 *
 * @remarks
 * Serve MCP servers from a specific registry by slug
 */
export declare function useServeMCPRegistrySuspense(
  request: operations.ServeMCPRegistryRequest,
  security?: operations.ServeMCPRegistrySecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ServeMCPRegistryQueryData,
    ServeMCPRegistryQueryError
  >,
): UseSuspenseQueryResult<
  ServeMCPRegistryQueryData,
  ServeMCPRegistryQueryError
>;
export declare function setServeMCPRegistryData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      registrySlug: string;
      search?: string | undefined;
      cursor?: string | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ServeMCPRegistryQueryData,
): ServeMCPRegistryQueryData | undefined;
export declare function invalidateServeMCPRegistry(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        registrySlug: string;
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
export declare function invalidateAllServeMCPRegistry(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=serveMCPRegistry.d.ts.map
