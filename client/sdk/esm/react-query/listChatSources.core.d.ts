import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListSourcesResult } from "../models/components/listsourcesresult.js";
import { ListChatSourcesRequest, ListChatSourcesSecurity } from "../models/operations/listchatsources.js";
export type ListChatSourcesQueryData = ListSourcesResult;
export declare function prefetchListChatSources(queryClient: QueryClient, client$: GramCore, request?: ListChatSourcesRequest | undefined, security?: ListChatSourcesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListChatSourcesQuery(client$: GramCore, request?: ListChatSourcesRequest | undefined, security?: ListChatSourcesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListChatSourcesQueryData>;
};
export declare function queryKeyListChatSources(parameters: {
    gramSession?: string | undefined;
    gramProject?: string | undefined;
    gramChatSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listChatSources.core.d.ts.map