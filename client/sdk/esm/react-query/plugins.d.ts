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
  ListPluginsRequest,
  ListPluginsSecurity,
} from "../models/operations/listplugins.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildPluginsQuery,
  PluginsQueryData,
  prefetchPlugins,
  queryKeyPlugins,
} from "./plugins.core.js";
export {
  buildPluginsQuery,
  type PluginsQueryData,
  prefetchPlugins,
  queryKeyPlugins,
};
export type PluginsQueryError =
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
 * listPlugins plugins
 *
 * @remarks
 * List all plugins for the current project.
 */
export declare function usePlugins(
  request?: ListPluginsRequest | undefined,
  security?: ListPluginsSecurity | undefined,
  options?: QueryHookOptions<PluginsQueryData, PluginsQueryError>,
): UseQueryResult<PluginsQueryData, PluginsQueryError>;
/**
 * listPlugins plugins
 *
 * @remarks
 * List all plugins for the current project.
 */
export declare function usePluginsSuspense(
  request?: ListPluginsRequest | undefined,
  security?: ListPluginsSecurity | undefined,
  options?: SuspenseQueryHookOptions<PluginsQueryData, PluginsQueryError>,
): UseSuspenseQueryResult<PluginsQueryData, PluginsQueryError>;
export declare function setPluginsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: PluginsQueryData,
): PluginsQueryData | undefined;
export declare function invalidatePlugins(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllPlugins(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=plugins.d.ts.map
