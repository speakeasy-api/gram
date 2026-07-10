import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { DownloadCodexInstallScriptRequest, DownloadCodexInstallScriptResponse, DownloadCodexInstallScriptSecurity } from "../models/operations/downloadcodexinstallscript.js";
export type PluginsDownloadCodexInstallScriptQueryData = DownloadCodexInstallScriptResponse;
export declare function prefetchPluginsDownloadCodexInstallScript(queryClient: QueryClient, client$: GramCore, request?: DownloadCodexInstallScriptRequest | undefined, security?: DownloadCodexInstallScriptSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildPluginsDownloadCodexInstallScriptQuery(client$: GramCore, request?: DownloadCodexInstallScriptRequest | undefined, security?: DownloadCodexInstallScriptSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<PluginsDownloadCodexInstallScriptQueryData>;
};
export declare function queryKeyPluginsDownloadCodexInstallScript(parameters: {
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=pluginsDownloadCodexInstallScript.core.d.ts.map