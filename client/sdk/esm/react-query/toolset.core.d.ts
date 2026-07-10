import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
import { GetToolsetRequest, GetToolsetSecurity } from "../models/operations/gettoolset.js";
export type ToolsetQueryData = Toolset;
export declare function prefetchToolset(queryClient: QueryClient, client$: GramCore, request: GetToolsetRequest, security?: GetToolsetSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildToolsetQuery(client$: GramCore, request: GetToolsetRequest, security?: GetToolsetSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ToolsetQueryData>;
};
export declare function queryKeyToolset(parameters: {
    slug: string;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=toolset.core.d.ts.map