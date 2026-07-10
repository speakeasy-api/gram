import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListSessionsResult } from "../models/components/listsessionsresult.js";
import { ListSessionsRequest, ListSessionsSecurity } from "../models/operations/listsessions.js";
export type ListSessionsQueryData = ListSessionsResult;
export declare function prefetchListSessions(queryClient: QueryClient, client$: GramCore, request: ListSessionsRequest, security?: ListSessionsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListSessionsQuery(client$: GramCore, request: ListSessionsRequest, security?: ListSessionsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListSessionsQueryData>;
};
export declare function queryKeyListSessions(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listSessions.core.d.ts.map