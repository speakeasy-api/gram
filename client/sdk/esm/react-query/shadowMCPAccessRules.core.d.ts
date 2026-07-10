import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListShadowMCPAccessRulesResult } from "../models/components/listshadowmcpaccessrulesresult.js";
import { AccessScope, Disposition, ListShadowMCPAccessRulesRequest, ListShadowMCPAccessRulesSecurity } from "../models/operations/listshadowmcpaccessrules.js";
export type ShadowMCPAccessRulesQueryData = ListShadowMCPAccessRulesResult;
export declare function prefetchShadowMCPAccessRules(queryClient: QueryClient, client$: GramCore, request?: ListShadowMCPAccessRulesRequest | undefined, security?: ListShadowMCPAccessRulesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildShadowMCPAccessRulesQuery(client$: GramCore, request?: ListShadowMCPAccessRulesRequest | undefined, security?: ListShadowMCPAccessRulesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ShadowMCPAccessRulesQueryData>;
};
export declare function queryKeyShadowMCPAccessRules(parameters: {
    disposition?: Disposition | undefined;
    accessScope?: AccessScope | undefined;
    projectId?: string | undefined;
    limit?: number | undefined;
    cursor?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=shadowMCPAccessRules.core.d.ts.map