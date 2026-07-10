import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { DownloadObservabilityPluginRequest, DownloadObservabilityPluginResponse, DownloadObservabilityPluginSecurity, Platform } from "../models/operations/downloadobservabilityplugin.js";
export type PluginsDownloadObservabilityPluginQueryData = DownloadObservabilityPluginResponse;
export declare function prefetchPluginsDownloadObservabilityPlugin(queryClient: QueryClient, client$: GramCore, request: DownloadObservabilityPluginRequest, security?: DownloadObservabilityPluginSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildPluginsDownloadObservabilityPluginQuery(client$: GramCore, request: DownloadObservabilityPluginRequest, security?: DownloadObservabilityPluginSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<PluginsDownloadObservabilityPluginQueryData>;
};
export declare function queryKeyPluginsDownloadObservabilityPlugin(parameters: {
    platform: Platform;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=pluginsDownloadObservabilityPlugin.core.d.ts.map