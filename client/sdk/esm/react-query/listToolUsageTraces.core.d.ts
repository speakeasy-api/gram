import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolUsageTracesResult } from "../models/components/listtoolusagetracesresult.js";
import { ListToolUsageTracesRequest, ListToolUsageTracesSecurity } from "../models/operations/listtoolusagetraces.js";
export type ListToolUsageTracesQueryData = ListToolUsageTracesResult;
export declare function prefetchListToolUsageTraces(queryClient: QueryClient, client$: GramCore, request: ListToolUsageTracesRequest, security?: ListToolUsageTracesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListToolUsageTracesQuery(client$: GramCore, request: ListToolUsageTracesRequest, security?: ListToolUsageTracesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListToolUsageTracesQueryData>;
};
export declare function queryKeyListToolUsageTraces(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listToolUsageTraces.core.d.ts.map