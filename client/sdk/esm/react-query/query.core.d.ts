import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type QueryQueryData = components.QueryResult;
export declare function prefetchQuery(queryClient: QueryClient, client$: GramCore, request: operations.QueryRequest, security?: operations.QuerySecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildQueryQuery(client$: GramCore, request: operations.QueryRequest, security?: operations.QuerySecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<QueryQueryData>;
};
export declare function queryKeyQuery(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=query.core.d.ts.map