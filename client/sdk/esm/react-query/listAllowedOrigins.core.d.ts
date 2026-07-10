import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAllowedOriginsResult } from "../models/components/listallowedoriginsresult.js";
import { ListAllowedOriginsRequest, ListAllowedOriginsSecurity } from "../models/operations/listallowedorigins.js";
export type ListAllowedOriginsQueryData = ListAllowedOriginsResult;
export declare function prefetchListAllowedOrigins(queryClient: QueryClient, client$: GramCore, request?: ListAllowedOriginsRequest | undefined, security?: ListAllowedOriginsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListAllowedOriginsQuery(client$: GramCore, request?: ListAllowedOriginsRequest | undefined, security?: ListAllowedOriginsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListAllowedOriginsQueryData>;
};
export declare function queryKeyListAllowedOrigins(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listAllowedOrigins.core.d.ts.map