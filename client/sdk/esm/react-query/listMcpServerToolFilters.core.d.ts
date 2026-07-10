import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolFiltersResult } from "../models/components/listtoolfiltersresult.js";
import { ListMcpServerToolFiltersRequest, ListMcpServerToolFiltersSecurity } from "../models/operations/listmcpservertoolfilters.js";
export type ListMcpServerToolFiltersQueryData = ListToolFiltersResult;
export declare function prefetchListMcpServerToolFilters(queryClient: QueryClient, client$: GramCore, request?: ListMcpServerToolFiltersRequest | undefined, security?: ListMcpServerToolFiltersSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListMcpServerToolFiltersQuery(client$: GramCore, request?: ListMcpServerToolFiltersRequest | undefined, security?: ListMcpServerToolFiltersSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListMcpServerToolFiltersQueryData>;
};
export declare function queryKeyListMcpServerToolFilters(parameters: {
    id?: string | undefined;
    slug?: string | undefined;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listMcpServerToolFilters.core.d.ts.map