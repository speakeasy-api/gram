import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetPluginsResult } from "../models/components/getpluginsresult.js";
import { GetAgentPluginsRequest, GetAgentPluginsSecurity } from "../models/operations/getagentplugins.js";
export type AgentPluginsQueryData = GetPluginsResult;
export declare function prefetchAgentPlugins(queryClient: QueryClient, client$: GramCore, request: GetAgentPluginsRequest, security?: GetAgentPluginsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildAgentPluginsQuery(client$: GramCore, request: GetAgentPluginsRequest, security?: GetAgentPluginsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<AgentPluginsQueryData>;
};
export declare function queryKeyAgentPlugins(parameters: {
    email: string;
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=agentPlugins.core.d.ts.map