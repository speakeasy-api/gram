import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAssetsResult } from "../models/components/listassetsresult.js";
import { ListAssetsRequest, ListAssetsSecurity } from "../models/operations/listassets.js";
export type ListAssetsQueryData = ListAssetsResult;
export declare function prefetchListAssets(queryClient: QueryClient, client$: GramCore, request?: ListAssetsRequest | undefined, security?: ListAssetsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListAssetsQuery(client$: GramCore, request?: ListAssetsRequest | undefined, security?: ListAssetsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListAssetsQueryData>;
};
export declare function queryKeyListAssets(parameters: {
    gramSession?: string | undefined;
    gramProject?: string | undefined;
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listAssets.core.d.ts.map