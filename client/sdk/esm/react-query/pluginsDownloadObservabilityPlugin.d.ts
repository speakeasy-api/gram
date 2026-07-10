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
  DownloadObservabilityPluginRequest,
  DownloadObservabilityPluginSecurity,
  Platform,
} from "../models/operations/downloadobservabilityplugin.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildPluginsDownloadObservabilityPluginQuery,
  PluginsDownloadObservabilityPluginQueryData,
  prefetchPluginsDownloadObservabilityPlugin,
  queryKeyPluginsDownloadObservabilityPlugin,
} from "./pluginsDownloadObservabilityPlugin.core.js";
export {
  buildPluginsDownloadObservabilityPluginQuery,
  type PluginsDownloadObservabilityPluginQueryData,
  prefetchPluginsDownloadObservabilityPlugin,
  queryKeyPluginsDownloadObservabilityPlugin,
};
export type PluginsDownloadObservabilityPluginQueryError =
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
 * downloadObservabilityPlugin plugins
 *
 * @remarks
 * Download a ZIP of the per-org observability plugin (Gram hooks). Mints a fresh hooks-scoped API key on each download and embeds it in the plugin's hook script.
 */
export declare function usePluginsDownloadObservabilityPlugin(
  request: DownloadObservabilityPluginRequest,
  security?: DownloadObservabilityPluginSecurity | undefined,
  options?: QueryHookOptions<
    PluginsDownloadObservabilityPluginQueryData,
    PluginsDownloadObservabilityPluginQueryError
  >,
): UseQueryResult<
  PluginsDownloadObservabilityPluginQueryData,
  PluginsDownloadObservabilityPluginQueryError
>;
/**
 * downloadObservabilityPlugin plugins
 *
 * @remarks
 * Download a ZIP of the per-org observability plugin (Gram hooks). Mints a fresh hooks-scoped API key on each download and embeds it in the plugin's hook script.
 */
export declare function usePluginsDownloadObservabilityPluginSuspense(
  request: DownloadObservabilityPluginRequest,
  security?: DownloadObservabilityPluginSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    PluginsDownloadObservabilityPluginQueryData,
    PluginsDownloadObservabilityPluginQueryError
  >,
): UseSuspenseQueryResult<
  PluginsDownloadObservabilityPluginQueryData,
  PluginsDownloadObservabilityPluginQueryError
>;
export declare function setPluginsDownloadObservabilityPluginData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      platform: Platform;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: PluginsDownloadObservabilityPluginQueryData,
): PluginsDownloadObservabilityPluginQueryData | undefined;
export declare function invalidatePluginsDownloadObservabilityPlugin(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        platform: Platform;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllPluginsDownloadObservabilityPlugin(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=pluginsDownloadObservabilityPlugin.d.ts.map
