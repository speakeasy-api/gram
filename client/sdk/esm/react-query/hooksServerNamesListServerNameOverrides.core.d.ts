import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ServerNameOverride } from "../models/components/servernameoverride.js";
import { ListServerNameOverridesRequest, ListServerNameOverridesSecurity } from "../models/operations/listservernameoverrides.js";
export type HooksServerNamesListServerNameOverridesQueryData = Array<ServerNameOverride>;
export declare function prefetchHooksServerNamesListServerNameOverrides(queryClient: QueryClient, client$: GramCore, request?: ListServerNameOverridesRequest | undefined, security?: ListServerNameOverridesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildHooksServerNamesListServerNameOverridesQuery(client$: GramCore, request?: ListServerNameOverridesRequest | undefined, security?: ListServerNameOverridesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<HooksServerNamesListServerNameOverridesQueryData>;
};
export declare function queryKeyHooksServerNamesListServerNameOverrides(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=hooksServerNamesListServerNameOverrides.core.d.ts.map