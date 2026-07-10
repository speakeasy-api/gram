import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListVariationsResult } from "../models/components/listvariationsresult.js";
import { ListGlobalVariationsRequest, ListGlobalVariationsSecurity } from "../models/operations/listglobalvariations.js";
export type GlobalVariationsQueryData = ListVariationsResult;
export declare function prefetchGlobalVariations(queryClient: QueryClient, client$: GramCore, request?: ListGlobalVariationsRequest | undefined, security?: ListGlobalVariationsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGlobalVariationsQuery(client$: GramCore, request?: ListGlobalVariationsRequest | undefined, security?: ListGlobalVariationsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GlobalVariationsQueryData>;
};
export declare function queryKeyGlobalVariations(parameters: {
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=globalVariations.core.d.ts.map