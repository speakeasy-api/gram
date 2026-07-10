import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListMcpEndpointsResult } from "../models/components/listmcpendpointsresult.js";
import { ListMcpEndpointsRequest, ListMcpEndpointsSecurity } from "../models/operations/listmcpendpoints.js";
export type McpEndpointsQueryData = ListMcpEndpointsResult;
export declare function prefetchMcpEndpoints(queryClient: QueryClient, client$: GramCore, request?: ListMcpEndpointsRequest | undefined, security?: ListMcpEndpointsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildMcpEndpointsQuery(client$: GramCore, request?: ListMcpEndpointsRequest | undefined, security?: ListMcpEndpointsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<McpEndpointsQueryData>;
};
export declare function queryKeyMcpEndpoints(parameters: {
    mcpServerId?: string | undefined;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=mcpEndpoints.core.d.ts.map