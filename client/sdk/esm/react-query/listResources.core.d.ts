import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListResourcesResult } from "../models/components/listresourcesresult.js";
import { ListResourcesRequest, ListResourcesSecurity } from "../models/operations/listresources.js";
export type ListResourcesQueryData = ListResourcesResult;
export declare function prefetchListResources(queryClient: QueryClient, client$: GramCore, request?: ListResourcesRequest | undefined, security?: ListResourcesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListResourcesQuery(client$: GramCore, request?: ListResourcesRequest | undefined, security?: ListResourcesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListResourcesQueryData>;
};
export declare function queryKeyListResources(parameters: {
    cursor?: string | undefined;
    limit?: number | undefined;
    deploymentId?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listResources.core.d.ts.map