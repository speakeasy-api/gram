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
  DownloadCodexInstallScriptRequest,
  DownloadCodexInstallScriptSecurity,
} from "../models/operations/downloadcodexinstallscript.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildPluginsDownloadCodexInstallScriptQuery,
  PluginsDownloadCodexInstallScriptQueryData,
  prefetchPluginsDownloadCodexInstallScript,
  queryKeyPluginsDownloadCodexInstallScript,
} from "./pluginsDownloadCodexInstallScript.core.js";
export {
  buildPluginsDownloadCodexInstallScriptQuery,
  type PluginsDownloadCodexInstallScriptQueryData,
  prefetchPluginsDownloadCodexInstallScript,
  queryKeyPluginsDownloadCodexInstallScript,
};
export type PluginsDownloadCodexInstallScriptQueryError =
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
 * downloadCodexInstallScript plugins
 *
 * @remarks
 * Download a bash install script that registers the Codex observability marketplace and pre-approves all hook events. Requires a published marketplace.
 */
export declare function usePluginsDownloadCodexInstallScript(
  request?: DownloadCodexInstallScriptRequest | undefined,
  security?: DownloadCodexInstallScriptSecurity | undefined,
  options?: QueryHookOptions<
    PluginsDownloadCodexInstallScriptQueryData,
    PluginsDownloadCodexInstallScriptQueryError
  >,
): UseQueryResult<
  PluginsDownloadCodexInstallScriptQueryData,
  PluginsDownloadCodexInstallScriptQueryError
>;
/**
 * downloadCodexInstallScript plugins
 *
 * @remarks
 * Download a bash install script that registers the Codex observability marketplace and pre-approves all hook events. Requires a published marketplace.
 */
export declare function usePluginsDownloadCodexInstallScriptSuspense(
  request?: DownloadCodexInstallScriptRequest | undefined,
  security?: DownloadCodexInstallScriptSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    PluginsDownloadCodexInstallScriptQueryData,
    PluginsDownloadCodexInstallScriptQueryError
  >,
): UseSuspenseQueryResult<
  PluginsDownloadCodexInstallScriptQueryData,
  PluginsDownloadCodexInstallScriptQueryError
>;
export declare function setPluginsDownloadCodexInstallScriptData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: PluginsDownloadCodexInstallScriptQueryData,
): PluginsDownloadCodexInstallScriptQueryData | undefined;
export declare function invalidatePluginsDownloadCodexInstallScript(
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
export declare function invalidateAllPluginsDownloadCodexInstallScript(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=pluginsDownloadCodexInstallScript.d.ts.map
