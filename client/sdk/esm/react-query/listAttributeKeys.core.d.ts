import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAttributeKeysResult } from "../models/components/listattributekeysresult.js";
import { ListAttributeKeysRequest, ListAttributeKeysSecurity } from "../models/operations/listattributekeys.js";
export type ListAttributeKeysQueryData = ListAttributeKeysResult;
export declare function prefetchListAttributeKeys(queryClient: QueryClient, client$: GramCore, request: ListAttributeKeysRequest, security?: ListAttributeKeysSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListAttributeKeysQuery(client$: GramCore, request: ListAttributeKeysRequest, security?: ListAttributeKeysSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListAttributeKeysQueryData>;
};
export declare function queryKeyListAttributeKeys(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listAttributeKeys.core.d.ts.map