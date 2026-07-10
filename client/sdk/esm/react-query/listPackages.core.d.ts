import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListPackagesResult } from "../models/components/listpackagesresult.js";
import { ListPackagesRequest, ListPackagesSecurity } from "../models/operations/listpackages.js";
export type ListPackagesQueryData = ListPackagesResult;
export declare function prefetchListPackages(queryClient: QueryClient, client$: GramCore, request?: ListPackagesRequest | undefined, security?: ListPackagesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListPackagesQuery(client$: GramCore, request?: ListPackagesRequest | undefined, security?: ListPackagesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListPackagesQueryData>;
};
export declare function queryKeyListPackages(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listPackages.core.d.ts.map