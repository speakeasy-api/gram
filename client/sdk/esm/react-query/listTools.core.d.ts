import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolsResult } from "../models/components/listtoolsresult.js";
import { ListToolsRequest, ListToolsSecurity, ToolTypes } from "../models/operations/listtools.js";
export type ListToolsQueryData = ListToolsResult;
export declare function prefetchListTools(queryClient: QueryClient, client$: GramCore, request?: ListToolsRequest | undefined, security?: ListToolsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListToolsQuery(client$: GramCore, request?: ListToolsRequest | undefined, security?: ListToolsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListToolsQueryData>;
};
export declare function queryKeyListTools(parameters: {
    cursor?: string | undefined;
    limit?: number | undefined;
    deploymentId?: string | undefined;
    urnPrefix?: string | undefined;
    toolTypes?: Array<ToolTypes> | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listTools.core.d.ts.map