import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  DownloadPluginPackageRequest,
  DownloadPluginPackageResponse,
  DownloadPluginPackageSecurity,
  QueryParamPlatform,
} from "../models/operations/downloadpluginpackage.js";
export type PluginsDownloadPluginPackageQueryData =
  DownloadPluginPackageResponse;
export declare function prefetchPluginsDownloadPluginPackage(
  queryClient: QueryClient,
  client$: GramCore,
  request: DownloadPluginPackageRequest,
  security?: DownloadPluginPackageSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildPluginsDownloadPluginPackageQuery(
  client$: GramCore,
  request: DownloadPluginPackageRequest,
  security?: DownloadPluginPackageSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<PluginsDownloadPluginPackageQueryData>;
};
export declare function queryKeyPluginsDownloadPluginPackage(parameters: {
  pluginId: string;
  platform: QueryParamPlatform;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=pluginsDownloadPluginPackage.core.d.ts.map
