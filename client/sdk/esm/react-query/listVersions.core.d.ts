import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListVersionsResult } from "../models/components/listversionsresult.js";
import { ListVersionsRequest, ListVersionsSecurity } from "../models/operations/listversions.js";
export type ListVersionsQueryData = ListVersionsResult;
export declare function prefetchListVersions(queryClient: QueryClient, client$: GramCore, request: ListVersionsRequest, security?: ListVersionsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListVersionsQuery(client$: GramCore, request: ListVersionsRequest, security?: ListVersionsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListVersionsQueryData>;
};
export declare function queryKeyListVersions(parameters: {
    name: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listVersions.core.d.ts.map