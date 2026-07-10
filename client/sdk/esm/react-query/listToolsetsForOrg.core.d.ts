import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolsetSummariesResult } from "../models/components/listtoolsetsummariesresult.js";
import { ListToolsetsForOrgRequest, ListToolsetsForOrgSecurity } from "../models/operations/listtoolsetsfororg.js";
export type ListToolsetsForOrgQueryData = ListToolsetSummariesResult;
export declare function prefetchListToolsetsForOrg(queryClient: QueryClient, client$: GramCore, request?: ListToolsetsForOrgRequest | undefined, security?: ListToolsetsForOrgSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListToolsetsForOrgQuery(client$: GramCore, request?: ListToolsetsForOrgRequest | undefined, security?: ListToolsetsForOrgSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListToolsetsForOrgQueryData>;
};
export declare function queryKeyListToolsetsForOrg(parameters: {
    gramSession?: string | undefined;
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listToolsetsForOrg.core.d.ts.map