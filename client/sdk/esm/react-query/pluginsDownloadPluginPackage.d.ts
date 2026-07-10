import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DownloadPluginPackageRequest, DownloadPluginPackageSecurity, QueryParamPlatform } from "../models/operations/downloadpluginpackage.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildPluginsDownloadPluginPackageQuery, PluginsDownloadPluginPackageQueryData, prefetchPluginsDownloadPluginPackage, queryKeyPluginsDownloadPluginPackage } from "./pluginsDownloadPluginPackage.core.js";
export { buildPluginsDownloadPluginPackageQuery, type PluginsDownloadPluginPackageQueryData, prefetchPluginsDownloadPluginPackage, queryKeyPluginsDownloadPluginPackage, };
export type PluginsDownloadPluginPackageQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * downloadPluginPackage plugins
 *
 * @remarks
 * Download a ZIP of a single plugin package for direct installation.
 */
export declare function usePluginsDownloadPluginPackage(request: DownloadPluginPackageRequest, security?: DownloadPluginPackageSecurity | undefined, options?: QueryHookOptions<PluginsDownloadPluginPackageQueryData, PluginsDownloadPluginPackageQueryError>): UseQueryResult<PluginsDownloadPluginPackageQueryData, PluginsDownloadPluginPackageQueryError>;
/**
 * downloadPluginPackage plugins
 *
 * @remarks
 * Download a ZIP of a single plugin package for direct installation.
 */
export declare function usePluginsDownloadPluginPackageSuspense(request: DownloadPluginPackageRequest, security?: DownloadPluginPackageSecurity | undefined, options?: SuspenseQueryHookOptions<PluginsDownloadPluginPackageQueryData, PluginsDownloadPluginPackageQueryError>): UseSuspenseQueryResult<PluginsDownloadPluginPackageQueryData, PluginsDownloadPluginPackageQueryError>;
export declare function setPluginsDownloadPluginPackageData(client: QueryClient, queryKeyBase: [
    parameters: {
        pluginId: string;
        platform: QueryParamPlatform;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: PluginsDownloadPluginPackageQueryData): PluginsDownloadPluginPackageQueryData | undefined;
export declare function invalidatePluginsDownloadPluginPackage(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        pluginId: string;
        platform: QueryParamPlatform;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllPluginsDownloadPluginPackage(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=pluginsDownloadPluginPackage.d.ts.map